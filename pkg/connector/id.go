package connector

import (
	"github.com/duo/matrix-octopus/pkg/octopus"
	"github.com/duo/matrix-octopus/pkg/octopusid"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

func (oc *OctopusClient) selfEventSender() bridgev2.EventSender {
	return bridgev2.EventSender{
		IsFromMe:    true,
		Sender:      networkid.UserID(oc.UserLogin.ID),
		SenderLogin: oc.UserLogin.ID,
	}
}

func (oc *OctopusClient) makeEventSender(id string) bridgev2.EventSender {
	return bridgev2.EventSender{
		IsFromMe:    octopusid.MakeUserLoginID(id) == oc.UserLogin.ID,
		Sender:      octopusid.MakeUserID(id),
		SenderLogin: octopusid.MakeUserLoginID(id),
	}
}

func (oc *OctopusClient) makePortalKey(chat *octopus.Chat) networkid.PortalKey {
	key := networkid.PortalKey{ID: networkid.PortalID(chat.ID)}
	// For non-group chats, add receiver
	if chat.Type != octopus.ChatGroup {
		key.Receiver = oc.UserLogin.ID
	}
	return key
}

func (oc *OctopusClient) makeDMPortalKey(identifier string) networkid.PortalKey {
	return networkid.PortalKey{
		ID:       networkid.PortalID(identifier),
		Receiver: oc.UserLogin.ID,
	}
}
