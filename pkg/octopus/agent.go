package octopus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

var (
	transmitTimeout = 3 * time.Minute
)

type Agent struct {
	log zerolog.Logger

	index int

	conn      *websocket.Conn
	writeLock sync.Mutex

	requests     map[int64]chan<- *Packet
	requestsLock sync.RWMutex
	requestID    int64

	clients     map[string]*OctopusClient
	clientsLock sync.RWMutex
}

func newAgent(log zerolog.Logger, conn *websocket.Conn) *Agent {
	return &Agent{
		log:       log.With().Str("octopus_agent", conn.RemoteAddr().String()).Logger(),
		conn:      conn,
		requests:  make(map[int64]chan<- *Packet),
		requestID: 0,
		clients:   make(map[string]*OctopusClient),
	}
}

func (a *Agent) transmit(mxid string, pktType PacketType, payload any) (any, error) {
	a.writeLock.Lock()
	defer a.writeLock.Unlock()

	if a.conn == nil {
		return nil, errors.New("no octopus agent connection avaiable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), transmitTimeout)
	defer cancel()

	packet := &Packet{
		ID:      atomic.AddInt64(&a.requestID, 1),
		MXID:    mxid,
		Type:    pktType,
		Payload: payload,
	}
	respChan := make(chan *Packet, 1)

	a.addResponseWaiter(packet.ID, respChan)
	defer a.removeResponseWaiter(packet.ID, respChan)

	a.log.Debug().Msgf("Transmit packet #%d %s", packet.ID, packet.Type)
	if err := a.conn.WriteJSON(packet); err != nil {
		a.log.Debug().Msgf("Transmit packet #%d failed, error: %v", packet.ID, err)
		return nil, err
	}

	select {
	case respPkt := <-respChan:
		a.log.Debug().Msgf("Receive packet #%d %s", packet.ID, respPkt.Type)
		resp, ok := respPkt.Payload.(*Response)
		if ok {
			if resp.Error != nil {
				a.log.Debug().Msgf("Receive packet #%d response failed, error: %v", packet.ID, resp.Error)
				return nil, resp.Error
			} else {
				return resp.Data, nil
			}
		} else {
			a.log.Debug().Msgf("Receive packet #%d invalid response", packet.ID)
			return nil, errors.New("receive invalid response packet")
		}
	case <-ctx.Done():
		a.log.Debug().Msgf("Receive packet #%d response timeout", packet.ID)
		return nil, ctx.Err()
	}
}

func (a *Agent) handleResponse(pkt *Packet) {
	a.requestsLock.RLock()
	respChan, ok := a.requests[pkt.ID]
	a.requestsLock.RUnlock()

	if ok {
		select {
		case respChan <- pkt:
		default:
			a.log.Warn().Msgf("Failed to handle response to %d: channel didn't accept response", pkt.ID)
		}
	} else {
		a.log.Warn().Msgf("Dropping response to %d: unknown request ID", pkt.ID)
	}
}

func (a *Agent) addResponseWaiter(reqID int64, waiter chan<- *Packet) {
	a.requestsLock.Lock()
	a.requests[reqID] = waiter
	a.requestsLock.Unlock()
}

func (a *Agent) removeResponseWaiter(reqID int64, waiter chan<- *Packet) {
	a.requestsLock.Lock()
	existingWaiter, ok := a.requests[reqID]
	if ok && existingWaiter == waiter {
		delete(a.requests, reqID)
	}
	a.requestsLock.Unlock()
	close(waiter)
}

func (a *Agent) close() {
	a.writeLock.Lock()
	defer a.writeLock.Unlock()

	if a.conn != nil {
		msg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "")
		_ = a.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(3*time.Second))
		_ = a.conn.Close()

		a.conn = nil
	}
}
