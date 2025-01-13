package connector

import (
	"context"
	"fmt"
	"time"

	"github.com/duo/matrix-octopus/pkg/octopus"
	"github.com/duo/matrix-octopus/pkg/octopusid"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

func (oc *OctopusClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (message *bridgev2.MatrixMessageResponse, err error) {
	if !oc.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}

	octopusMsg, err := oc.Main.MsgConv.ToOctopus(ctx, oc.Client, msg.Event, msg.Content, msg.Portal)
	if err != nil {
		return nil, fmt.Errorf("failed to convert message: %w", err)
	}

	octopusMsg.ID = string(msg.Event.ID)
	octopusMsg.Timestamp = msg.Event.Timestamp
	octopusMsg.From = octopus.User{
		ID: oc.ID,
	}
	octopusMsg.Chat = octopus.Chat{
		ID: string(msg.Portal.ID),
	}

	meta := msg.Portal.Metadata.(*octopusid.PortalMetadata)
	switch meta.ChatType {
	case octopus.ChatPrivate:
		octopusMsg.Chat.Type = octopus.ChatPrivate
	case octopus.ChatGroup:
		octopusMsg.Chat.Type = octopus.ChatGroup
	default:
		return nil, fmt.Errorf("unknown chat type")
	}

	if msg.ReplyTo != nil {
		if msgID, err := octopusid.ParseMessageID(msg.ReplyTo.ID); err != nil {
			return nil, err
		} else {
			octopusMsg.Reply = &octopus.ReplyInfo{
				ID: msgID.ID,
			}
		}
	}

	zerolog.Ctx(ctx).Debug().Msgf("Send Octopus Message: %+v", octopusMsg)

	if resp, err := oc.Client.SendMessage(octopusMsg); err != nil {
		return nil, bridgev2.WrapErrorInStatus(err).WithSendNotice(true)
	} else {
		return &bridgev2.MatrixMessageResponse{
			DB: &database.Message{
				ID:        octopusid.MakeMessageID(octopusMsg.Chat.ID, resp.ID),
				SenderID:  octopusid.MakeUserID(oc.ID),
				Timestamp: time.UnixMilli(resp.Timestamp),
			},
			StreamOrder: time.UnixMilli(resp.Timestamp).Unix(),
		}, nil
	}
}
