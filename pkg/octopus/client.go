package octopus

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

type OctopusClient struct {
	log zerolog.Logger

	mxid string

	isLoggedIn atomic.Bool

	transmitFunc func(pktType PacketType, payload any) (any, error)
	processFunc  func(msg *Message)
	disposeFunc  func(err error)

	statusChannel chan bool
	cancel        context.CancelFunc
}

func (oc *OctopusClient) Connect() error {
	_, err := oc.transmitFunc(PktRequest, &Request{
		Type: MethodConnect,
	})

	return err
}

func (oc *OctopusClient) Disconnect() error {
	_, err := oc.transmitFunc(PktRequest, &Request{
		Type: MethodDisconnect,
	})

	return err
}

func (oc *OctopusClient) Login(step *LoginStep) (*LoginStep, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type:   MethodLogin,
		Params: step,
	})

	if resp != nil {
		return resp.(*LoginStep), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) IsLoggedIn() bool {
	return oc.isLoggedIn.Load()
}

func (oc *OctopusClient) GetLoginInfo() (*UserInfo, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type: MethodGetLoginInfo,
	})

	if resp != nil {
		oc.isLoggedIn.Store(true)
		return resp.(*UserInfo), err
	} else {
		oc.isLoggedIn.Store(false)
		return nil, err
	}
}

func (oc *OctopusClient) GetUserInfo(userID string) (*UserInfo, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type:   MethodGetUserInfo,
		Params: []string{userID},
	})

	if resp != nil {
		return resp.(*UserInfo), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) GetGroupInfo(groupID string) (*GroupInfo, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type:   MethodGetGroupInfo,
		Params: []string{groupID},
	})

	if resp != nil {
		return resp.(*GroupInfo), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) GetFriendList() ([]*UserInfo, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type: MethodGetFriendList,
	})

	if resp != nil {
		return resp.([]*UserInfo), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) GetGroupList() ([]*GroupInfo, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type: MethodGetGroupList,
	})

	if resp != nil {
		return resp.([]*GroupInfo), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) GetGroupMemberList(groupID string) ([]*MemberInfo, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type:   MethodGetGroupMemberList,
		Params: []string{groupID},
	})

	if resp != nil {
		return resp.([]*MemberInfo), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) GetGroupMemberInfo(groupID, userID string) (*MemberInfo, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type:   MethodGetGroupMemberInfo,
		Params: []string{groupID, userID},
	})

	if resp != nil {
		return resp.(*MemberInfo), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) SendMessage(msg *Message) (*Message, error) {
	resp, err := oc.transmitFunc(PktRequest, &Request{
		Type:   MethodSendMessage,
		Params: msg,
	})

	if resp != nil {
		return resp.(*Message), err
	} else {
		return nil, err
	}
}

func (oc *OctopusClient) RevokeMessage(messageID string) error {
	_, err := oc.transmitFunc(PktRequest, &Request{
		Type:   MethodRevokeMessage,
		Params: []string{messageID},
	})

	return err
}

func (oc *OctopusClient) Dispose(err error) {
	if oc.cancel != nil {
		oc.cancel()
		oc.cancel = nil
	}

	if oc.disposeFunc != nil {
		oc.disposeFunc(err)
	}
}

func newOctopusClient(
	log zerolog.Logger,
	mxid string,
	transmitFunc func(pktType PacketType, payload any) (any, error),
	processFunc func(msg *Message),
	disposeFunc func(err error)) *OctopusClient {
	return &OctopusClient{
		mxid:          mxid,
		log:           log.With().Str("octopus_client", mxid).Logger(),
		transmitFunc:  transmitFunc,
		processFunc:   processFunc,
		disposeFunc:   disposeFunc,
		statusChannel: make(chan bool),
	}
}

func (oc *OctopusClient) processNotice(notice *Notice) {
	switch notice.Type {
	case NoticeHeartbeat:
		heartbeat := notice.Data.(*Heartbeat)
		oc.isLoggedIn.Store(heartbeat.IsLoggedIn)

		if oc.cancel == nil {
			oc.startChecker(notice.ClientID, heartbeat.Interval)
		}
		oc.statusChannel <- heartbeat.IsLoggedIn
	}
}

func (oc *OctopusClient) startChecker(clientID string, interval uint32) {
	ctx, cancel := context.WithCancel(context.Background())
	oc.cancel = cancel

	checkInterval := time.Duration(3*interval) * time.Millisecond

	go func() {
		oc.log.Info().Str("Client", clientID).Msgf("Status checker started, interval: %v", checkInterval)

		for {
			select {
			case status := <-oc.statusChannel:
				oc.isLoggedIn.Store(status)
			case <-time.After(checkInterval):
				oc.isLoggedIn.Store(false)
			case <-ctx.Done():
				oc.log.Info().Str("Client", clientID).Msgf("Status checker stopped")
				return
			}
		}
	}()
}
