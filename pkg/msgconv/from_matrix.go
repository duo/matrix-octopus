package msgconv

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/duo/matrix-octopus/pkg/octopus"

	"github.com/gabriel-vasile/mimetype"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

type contextKey int

const (
	contextKeyClient contextKey = iota
	contextKeyIntent
	contextKeyPortal
)

func (mc *MessageConverter) ToOctopus(
	ctx context.Context,
	client *octopus.OctopusClient,
	evt *event.Event,
	content *event.MessageEventContent,
	portal *bridgev2.Portal,
) (*octopus.Message, error) {
	ctx = context.WithValue(ctx, contextKeyClient, client)
	ctx = context.WithValue(ctx, contextKeyPortal, portal)

	if evt.Type == event.EventSticker {
		content.MsgType = event.MessageType(event.EventSticker.Type)
	}

	var message *octopus.Message

	switch content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		message = mc.constructTextMessage(ctx, content)
	case event.MessageType(event.EventSticker.Type), event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		data, err := mc.Bridge.Bot.DownloadMedia(ctx, content.URL, content.File)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", bridgev2.ErrMediaDownloadFailed, err)
		}
		message = mc.constructMediaMessage(ctx, content, data)
	case event.MsgLocation:
		lat, long, err := parseGeoURI(content.GeoURI)
		if err != nil {
			return nil, err
		}
		message = &octopus.Message{
			Type:     octopus.MsgLocation,
			Content:  content.Body,
			Mentions: []string{},
			Attachment: &octopus.LocationData{
				Name:      content.Body,
				Address:   content.Body,
				Longitude: long,
				Latitude:  lat,
			},
		}
	default:
		return nil, fmt.Errorf("%w %s", bridgev2.ErrUnsupportedMessageType, content.MsgType)

	}

	return message, nil
}

func (mc *MessageConverter) constructTextMessage(ctx context.Context, content *event.MessageEventContent) *octopus.Message {
	text, mentions := mc.parseText(ctx, content)
	if content.Mentions != nil && content.Mentions.Room {
		mentions = append(mentions, "room")
	}

	return &octopus.Message{
		Type:     octopus.MsgText,
		Content:  text,
		Mentions: mentions,
	}
}

func (mc *MessageConverter) constructMediaMessage(_ context.Context, content *event.MessageEventContent, data []byte) *octopus.Message {
	mime := content.GetInfo().MimeType
	if mime == "" {
		mime = mimetype.Detect(data).String()
	}

	fileName := content.Body
	if content.FileName != "" {
		fileName = content.FileName
	}

	message := &octopus.Message{
		Type:     octopus.MsgText,
		Content:  fileName,
		Mentions: []string{},
		Attachment: []*octopus.BlobData{
			{
				Name:   fileName,
				Mime:   mime,
				Binary: data,
			},
		},
	}

	switch content.MsgType {
	case event.MessageType(event.EventSticker.Type):
		message.Type = octopus.MsgSticker
	case event.MsgImage:
		message.Type = octopus.MsgImage
	case event.MsgVideo:
		message.Type = octopus.MsgVideo
	case event.MsgAudio:
		message.Type = octopus.MsgAudio
	case event.MsgFile:
		message.Type = octopus.MsgFile
	}

	return message
}

func (mc *MessageConverter) parseText(ctx context.Context, content *event.MessageEventContent) (text string, mentions []string) {
	mentions = make([]string, 0)

	parseCtx := format.NewContext(ctx)
	parseCtx.ReturnData["allowed_mentions"] = content.Mentions
	parseCtx.ReturnData["output_mentions"] = &mentions
	if content.Format == event.FormatHTML {
		text = mc.HTMLParser.Parse(content.FormattedBody, parseCtx)
	} else {
		text = content.Body
	}
	return
}

func (mc *MessageConverter) convertPill(displayname, mxid, eventID string, ctx format.Context) string {
	if len(mxid) == 0 || mxid[0] != '@' {
		return format.DefaultPillConverter(displayname, mxid, eventID, ctx)
	}
	allowedMentions, _ := ctx.ReturnData["allowed_mentions"].(*event.Mentions)
	if allowedMentions != nil && !allowedMentions.Has(id.UserID(mxid)) {
		return displayname
	}
	var oid string
	ghost, err := mc.Bridge.GetGhostByMXID(ctx.Ctx, id.UserID(mxid))
	if err != nil {
		zerolog.Ctx(ctx.Ctx).Err(err).Str("mxid", mxid).Msg("Failed to get ghost for mention")
		return displayname
	} else if ghost != nil {
		oid = string(ghost.ID)
	} else if user, err := mc.Bridge.GetExistingUserByMXID(ctx.Ctx, id.UserID(mxid)); err != nil {
		zerolog.Ctx(ctx.Ctx).Err(err).Str("mxid", mxid).Msg("Failed to get user for mention")
		return displayname
	} else if user != nil {
		portal := getPortal(ctx.Ctx)
		login, _, _ := portal.FindPreferredLogin(ctx.Ctx, user, false)
		if login == nil {
			return displayname
		}
		oid = string(login.ID)
	} else {
		return displayname
	}
	mentions := ctx.ReturnData["output_mentions"].(*[]string)
	*mentions = append(*mentions, oid)
	return fmt.Sprintf("@%s", oid)
}

func parseGeoURI(uri string) (lat, long float64, err error) {
	if !strings.HasPrefix(uri, "geo:") {
		err = fmt.Errorf("uri doesn't have geo: prefix")
		return
	}
	// Remove geo: prefix and anything after ;
	coordinates := strings.Split(strings.TrimPrefix(uri, "geo:"), ";")[0]

	if splitCoordinates := strings.Split(coordinates, ","); len(splitCoordinates) != 2 {
		err = fmt.Errorf("didn't find exactly two numbers separated by a comma")
	} else if lat, err = strconv.ParseFloat(splitCoordinates[0], 64); err != nil {
		err = fmt.Errorf("latitude is not a number: %w", err)
	} else if long, err = strconv.ParseFloat(splitCoordinates[1], 64); err != nil {
		err = fmt.Errorf("longitude is not a number: %w", err)
	}
	return
}
