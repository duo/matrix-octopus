package connector

import (
	"github.com/duo/matrix-octopus/pkg/octopus"
	"maunium.net/go/mautrix/bridge/status"
)

func (oc *OctopusClient) handleOctopusMessage(rawMsg *octopus.Message) {
	log := oc.UserLogin.Log.With().
		Str("action", "handle_octopus_message").
		Str("sender_id", rawMsg.From.ID).
		Str("message_type", string(rawMsg.Type)).
		Logger()

	log.Debug().Msgf("Receive Octopus message: %+v", rawMsg)

	oc.Main.Bridge.QueueRemoteEvent(oc.UserLogin, &EnsureOctopusChatStateEvent{
		Chat: rawMsg.Chat,
		oc:   oc,
	})
	oc.Main.Bridge.QueueRemoteEvent(oc.UserLogin, &OctopusMessage{
		Message: rawMsg,
		oc:      oc,
	})
}

func (oc *OctopusClient) dispose(err error) {
	oc.Client = nil
	oc.UserLogin.BridgeState.Send(status.BridgeState{
		StateEvent: status.StateBadCredentials,
		Message:    err.Error(),
	})
}
