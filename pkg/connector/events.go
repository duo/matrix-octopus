package connector

import (
	"context"
	"fmt"
	"time"

	"github.com/duo/matrix-octopus/pkg/octopus"
	"github.com/duo/matrix-octopus/pkg/octopusid"

	"github.com/rs/zerolog"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

const softRevoke = true

type EnsureOctopusChatStateEvent struct {
	Chat       octopus.Chat
	oc         *OctopusClient
	postHandle func()
}

var (
	_ bridgev2.RemoteChatResyncWithInfo       = (*EnsureOctopusChatStateEvent)(nil)
	_ bridgev2.RemoteEventThatMayCreatePortal = (*EnsureOctopusChatStateEvent)(nil)
	_ bridgev2.RemotePostHandler              = (*EnsureOctopusChatStateEvent)(nil)
)

func (evt *EnsureOctopusChatStateEvent) GetType() bridgev2.RemoteEventType {
	return bridgev2.RemoteEventChatResync
}

func (evt *EnsureOctopusChatStateEvent) GetPortalKey() networkid.PortalKey {
	return evt.oc.makePortalKey(&evt.Chat)
}

func (evt *EnsureOctopusChatStateEvent) ShouldCreatePortal() bool {
	return true
}

func (evt *EnsureOctopusChatStateEvent) AddLogContext(c zerolog.Context) zerolog.Context {
	return c
}

func (evt *EnsureOctopusChatStateEvent) GetSender() bridgev2.EventSender {
	return bridgev2.EventSender{}
}

func (evt *EnsureOctopusChatStateEvent) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	switch evt.Chat.Type {
	case octopus.ChatPrivate:
		if portal.MXID != "" {
			return &bridgev2.ChatInfo{
				Type:         ptr.Ptr(database.RoomTypeDefault),
				ExtraUpdates: updateChatType(evt.Chat.Type),
			}, nil
		}
		return evt.oc.makeDirectChatInfo(evt.Chat.ID), nil
	case octopus.ChatGroup:
		if portal.MXID != "" {
			return &bridgev2.ChatInfo{
				Type:         ptr.Ptr(database.RoomTypeDefault),
				ExtraUpdates: updateChatType(evt.Chat.Type),
			}, nil
		}
		if info, err := evt.oc.Client.GetGroupInfo(evt.Chat.ID); err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to fetch Octopus group info")
			return nil, err
		} else {
			evt.postHandle = func() {
				evt.oc.updateMemberDisplyname(ctx, portal, evt.Chat.ID)
			}
			return evt.oc.wrapGroupChatInfo(info), nil
		}
	default:
		return nil, fmt.Errorf("unknown chat type")
	}
}

func (evt *EnsureOctopusChatStateEvent) PostHandle(ctx context.Context, portal *bridgev2.Portal) {
	if ph := evt.postHandle; ph != nil {
		evt.postHandle = nil
		ph()
	}
}

type OctopusMessage struct {
	*octopus.Message
	oc *OctopusClient
}

var (
	_ bridgev2.RemoteMessage            = (*OctopusMessage)(nil)
	_ bridgev2.RemoteEventWithTimestamp = (*OctopusMessage)(nil)
	_ bridgev2.RemoteMessageRemove      = (*OctopusMessage)(nil)
)

func (evt *OctopusMessage) ShouldCreatePortal() bool {
	return true
}

func (om *OctopusMessage) AddLogContext(c zerolog.Context) zerolog.Context {
	return c.Str("sender_id", om.From.ID).Int64("message_ts", om.Timestamp)
}

func (om *OctopusMessage) GetPortalKey() networkid.PortalKey {
	return om.oc.makePortalKey(&om.Chat)
}

func (om *OctopusMessage) GetSender() bridgev2.EventSender {
	return om.oc.makeEventSender(om.From.ID)
}

func (om *OctopusMessage) GetType() bridgev2.RemoteEventType {
	switch om.Type {
	case octopus.MsgRevoke:
		if softRevoke {
			return bridgev2.RemoteEventMessage
		} else {
			return bridgev2.RemoteEventMessageRemove
		}
	default:
		return bridgev2.RemoteEventMessage
	}
}

func (om *OctopusMessage) GetID() networkid.MessageID {
	if softRevoke && om.Type == octopus.MsgRevoke {
		return octopusid.MakeFakeMessageID(om.Chat.ID, "revoke-"+om.ID)
	}
	return octopusid.MakeMessageID(om.Chat.ID, om.ID)
}

func (om *OctopusMessage) GetTimestamp() time.Time {
	ts := om.Timestamp
	if ts == 0 {
		return time.Now()
	}
	return time.UnixMilli(ts)
}

func (om *OctopusMessage) GetTargetMessage() networkid.MessageID {
	return octopusid.MakeMessageID(om.Chat.ID, om.ID)
}

func (om *OctopusMessage) ConvertMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI) (*bridgev2.ConvertedMessage, error) {
	om.oc.EnqueuePortalResync(portal)
	return om.oc.Main.MsgConv.ToMatrix(ctx, om.oc.Client, portal, intent, om.Message), nil
}
