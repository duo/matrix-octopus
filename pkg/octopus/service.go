package octopus

import (
	"container/heap"
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

var (
	errMissingToken = ErrorResponse{
		HTTPStatus: http.StatusForbidden,
		Code:       "M_MISSING_TOKEN",
		Message:    "Missing authorization header",
	}
	errUnknownToken = ErrorResponse{
		HTTPStatus: http.StatusForbidden,
		Code:       "M_UNKNOWN_TOKEN",
		Message:    "Unknown authorization token",
	}

	upgrader = websocket.Upgrader{}
)

type NoAgentError struct {
	Message string
}

func (e *NoAgentError) Error() string {
	return e.Message
}

type OcotpusService struct {
	log zerolog.Logger

	addr   string
	secret string

	server *http.Server

	queue     *PriorityQueue
	queueLock sync.Mutex

	clients       map[string]*OctopusClient
	clientToAgent map[string]*Agent
	clientsLock   sync.Mutex
}

func NewOctopusService(log zerolog.Logger, addr, secret string) *OcotpusService {
	pq := &PriorityQueue{}
	heap.Init(pq)

	service := &OcotpusService{
		log:           log.With().Str("service", "Octopus").Logger(),
		addr:          addr,
		secret:        secret,
		clients:       make(map[string]*OctopusClient),
		clientToAgent: make(map[string]*Agent),
		queue:         pq,
	}
	service.server = &http.Server{
		Addr:    service.addr,
		Handler: service,
	}

	return service
}

func (os *OcotpusService) NewClient(mxid string, processFunc func(msg *Message), disposeFunc func(err error)) *OctopusClient {
	os.clientsLock.Lock()
	defer os.clientsLock.Unlock()

	client, ok := os.clients[mxid]
	if !ok {
		client = newOctopusClient(
			os.log,
			mxid,
			func(pktType PacketType, payload any) (any, error) {
				return os.transmit(mxid, pktType, payload)
			},
			processFunc,
			disposeFunc,
		)
		os.clients[mxid] = client
	} else {
		client.processFunc = processFunc
		client.disposeFunc = disposeFunc
	}

	return client
}

func (os *OcotpusService) Start() {
	os.log.Info().Msgf("OctopusService starting to listen on %s", os.addr)

	if err := os.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		os.log.Fatal().Msgf("Error in listener: %v", err)
	}
}

func (os *OcotpusService) Stop() {
	os.log.Info().Msgf("OctopusService stopping")

	os.queueLock.Lock()
	defer os.queueLock.Unlock()

	// Close all agents
	for _, agent := range *os.queue {
		agent.close()
	}

	// Shutdown server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := os.server.Shutdown(ctx)
	if err != nil {
		os.log.Warn().Msgf("Failed to shutdown server: %v", err)
	}
}

func (os *OcotpusService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Basic ") {
		errMissingToken.Write(w)
		return
	}

	if authHeader[len("Basic "):] != os.secret {
		errUnknownToken.Write(w)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		os.log.Warn().Msgf("Failed to upgrade websocket request: %v", err)
		return
	}

	remoteAddr := conn.RemoteAddr().String()
	os.log.Info().Msgf("Agent connected from %s", remoteAddr)

	agent := newAgent(os.log, conn)

	os.queueLock.Lock()
	heap.Push(os.queue, agent)
	os.queueLock.Unlock()

	defer func() {
		os.log.Info().Msgf("Agent disconnected from %s", remoteAddr)

		os.queueLock.Lock()
		os.clientsLock.Lock()
		agent.clientsLock.Lock()

		// Delete client agent mapping
		for _, client := range agent.clients {
			delete(os.clientToAgent, client.mxid)
		}
		// Clear agent clients
		agent.clients = map[string]*OctopusClient{}
		// Remove agent from queue
		os.queue.Remove(agent)

		agent.clientsLock.Unlock()
		os.clientsLock.Unlock()
		os.queueLock.Unlock()
	}()

	for {
		var pkt Packet
		err := conn.ReadJSON(&pkt)
		if err != nil {
			agent.log.Warn().Err(err).Msgf("Error reading from websocket")

			// Dispose all clients
			agent.clientsLock.Lock()
			for _, client := range agent.clients {
				go client.Dispose(err)
			}
			agent.clientsLock.Unlock()

			break
		}

		switch pkt.Type {
		case PktNotice:
			agent.clientsLock.RLock()
			client, ok := agent.clients[pkt.MXID]
			agent.clientsLock.RUnlock()
			if ok {
				go client.processNotice(pkt.Payload.(*Notice))
			} else {
				agent.log.Warn().Msgf("Dropping notice #%d for %s: no receiver", pkt.ID, pkt.MXID)
			}
		case PktMessage:
			agent.clientsLock.RLock()
			client, ok := agent.clients[pkt.MXID]
			agent.clientsLock.RUnlock()
			if ok {
				if client.processFunc != nil {
					go client.processFunc(pkt.Payload.(*Message))
				} else {
					client.log.Warn().Msgf("Dropping message #%d: no process function", pkt.ID)
				}
			} else {
				agent.log.Warn().Msgf("Dropping message #%d for %s: no receiver", pkt.ID, pkt.MXID)
			}
		case PktResponse:
			go agent.handleResponse(&pkt)
		}
	}
}

func (os *OcotpusService) transmit(mxid string, pktType PacketType, payload any) (any, error) {
	os.log.Debug().Msgf("Transmitting packet %s to %s, payload: %+v", pktType, mxid, payload)
	agent := os.assignAgent(mxid)
	if agent == nil {
		return nil, &NoAgentError{Message: "no octopus agent avaiable"}
	}

	return agent.transmit(mxid, pktType, payload)
}

func (os *OcotpusService) assignAgent(mxid string) *Agent {
	os.clientsLock.Lock()
	defer os.clientsLock.Unlock()

	agent, ok := os.clientToAgent[mxid]
	if ok {
		return agent
	}

	os.queueLock.Lock()
	defer os.queueLock.Unlock()

	if os.queue.Len() > 0 {
		agent = os.queue.Pop().(*Agent)

		agent.clientsLock.Lock()
		defer agent.clientsLock.Unlock()

		agent.clients[mxid] = os.clients[mxid]
		os.clientToAgent[mxid] = agent

		heap.Push(os.queue, agent)
	}

	return agent
}
