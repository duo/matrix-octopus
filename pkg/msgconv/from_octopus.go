package msgconv

import (
	"context"
	"fmt"
	"html"
	"math"
	"strings"

	"github.com/duo/matrix-octopus/pkg/octopus"
	"github.com/duo/matrix-octopus/pkg/octopusid"

	"github.com/gabriel-vasile/mimetype"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

func (mc *MessageConverter) ToMatrix(
	ctx context.Context,
	client *octopus.OctopusClient,
	portal *bridgev2.Portal,
	intent bridgev2.MatrixAPI,
	msg *octopus.Message,
) *bridgev2.ConvertedMessage {
	ctx = context.WithValue(ctx, contextKeyClient, client)
	ctx = context.WithValue(ctx, contextKeyIntent, intent)
	ctx = context.WithValue(ctx, contextKeyPortal, portal)

	var part *bridgev2.ConvertedMessagePart

	switch msg.Type {
	case octopus.MsgText:
		part = mc.convertTextMessage(ctx, msg)
	case octopus.MsgImage:
		part = mc.convertImageMessage(ctx, msg)
	case octopus.MsgSticker:
		part = mc.convertMediaMessage(ctx, msg, "sticker")[0]
	case octopus.MsgAudio:
		part = mc.convertMediaMessage(ctx, msg, "audio message")[0]
	case octopus.MsgVideo:
		part = mc.convertMediaMessage(ctx, msg, "video message")[0]
	case octopus.MsgFile:
		part = mc.convertMediaMessage(ctx, msg, "file attachment")[0]
	case octopus.MsgLocation:
		part = mc.convertLocationMessage(msg)
	case octopus.MsgApp:
		part = mc.convertAppMessage(msg)
	case octopus.MsgRevoke:
		part = mc.convertRevokeMessage(msg)
	default:
	}

	// Mentions
	part.Content.Mentions = &event.Mentions{}
	if msg.Mentions != nil {
		mc.addMentions(ctx, msg.Mentions, part.Content)
	}

	cm := &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{part},
	}

	// ReplyTo
	if msg.Reply != nil {
		cm.ReplyTo = &networkid.MessageOptionalPartID{
			MessageID: octopusid.MakeMessageID(msg.Chat.ID, msg.Reply.ID),
		}
	}

	return cm
}

type PreparedMedia struct {
	Type                       event.Type `json:"type"`
	*event.MessageEventContent `json:"content"`
	Extra                      map[string]any `json:"extra"`
	TypeDescription            string         `json:"type_description"`
}

func (pm *PreparedMedia) FillFileName() *PreparedMedia {
	if pm.FileName == "" {
		pm.FileName = strings.TrimPrefix(string(pm.MsgType), "m.") + mimetype.Lookup(pm.Info.MimeType).Extension()
	}
	return pm
}

func (mc *MessageConverter) convertTextMessage(_ context.Context, msg *octopus.Message) *bridgev2.ConvertedMessagePart {
	return &bridgev2.ConvertedMessagePart{
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			MsgType: event.MsgText,
			Body:    msg.Content,
		},
	}
}

func (mc *MessageConverter) convertImageMessage(ctx context.Context, msg *octopus.Message) *bridgev2.ConvertedMessagePart {
	parts := mc.convertMediaMessage(ctx, msg, "photo")
	if len(parts) == 1 {
		return parts[0]
	}

	var imagesMarkdown strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&imagesMarkdown, "![%s](%s)\n", part.Content.FileName, part.Content.URL)
	}

	rendered := format.RenderMarkdown(imagesMarkdown.String(), true, false)

	return &bridgev2.ConvertedMessagePart{
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			MsgType:       event.MsgText,
			Format:        event.FormatHTML,
			Body:          msg.Content,
			FormattedBody: fmt.Sprintf("%s\n%s", rendered.FormattedBody, msg.Content),
		},
	}
}

func (mc *MessageConverter) convertMediaMessage(ctx context.Context, msg *octopus.Message, typeName string) []*bridgev2.ConvertedMessagePart {
	blobs := msg.Attachment.([]*octopus.BlobData)

	parts := make([]*bridgev2.ConvertedMessagePart, 0, len(blobs))

	for _, blob := range blobs {
		preparedMedia := prepareMediaMessage(msg.Type, blob)
		preparedMedia.TypeDescription = typeName

		var part *bridgev2.ConvertedMessagePart
		if err := mc.reuploadOctopusAttachment(ctx, blob, preparedMedia); err != nil {
			part = mc.makeMediaFailure(ctx, preparedMedia, err)
		} else {
			part = &bridgev2.ConvertedMessagePart{
				Type:    preparedMedia.Type,
				Content: preparedMedia.MessageEventContent,
				Extra:   preparedMedia.Extra,
			}
		}

		parts = append(parts, part)
	}

	return parts
}

func (mc *MessageConverter) convertLocationMessage(msg *octopus.Message) *bridgev2.ConvertedMessagePart {
	location := msg.Attachment.(*octopus.LocationData)

	url := fmt.Sprintf("https://maps.google.com/?q=%.5f,%.5f", location.Latitude, location.Longitude)
	name := location.Name
	if len(name) == 0 {
		latChar := 'N'
		if location.Latitude < 0 {
			latChar = 'S'
		}
		longChar := 'E'
		if location.Longitude < 0 {
			longChar = 'W'
		}
		name = fmt.Sprintf("%.4f° %c %.4f° %c", math.Abs(location.Latitude), latChar, math.Abs(location.Longitude), longChar)
	}

	content := &event.MessageEventContent{
		MsgType:       event.MsgLocation,
		Body:          fmt.Sprintf("Location: %s\n%s\n%s", name, location.Address, url),
		Format:        event.FormatHTML,
		FormattedBody: fmt.Sprintf("Location: <a href='%s'>%s</a><br>%s", url, name, location.Address),
		GeoURI:        fmt.Sprintf("geo:%.5f,%.5f", location.Latitude, location.Longitude),
	}

	return &bridgev2.ConvertedMessagePart{
		Type:    event.EventMessage,
		Content: content,
	}
}

func (mc *MessageConverter) convertAppMessage(msg *octopus.Message) *bridgev2.ConvertedMessagePart {
	app := msg.Attachment.(*octopus.AppData)

	body := fmt.Sprintf("%s\n\n%s\n\n%s", app.Title, app.Description, app.URL)
	rendered := format.RenderMarkdown(
		fmt.Sprintf("**%s**\n%s\n\n[%s](%s)", app.Title, app.Description, app.URL, app.URL),
		true,
		false,
	)

	return &bridgev2.ConvertedMessagePart{
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			Body:          body,
			MsgType:       event.MsgText,
			Format:        event.FormatHTML,
			FormattedBody: rendered.FormattedBody,
		},
	}
}

func (mc *MessageConverter) convertRevokeMessage(msg *octopus.Message) *bridgev2.ConvertedMessagePart {
	msg.Reply = &octopus.ReplyInfo{
		ID: msg.ID,
	}

	return &bridgev2.ConvertedMessagePart{
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			MsgType:       event.MsgNotice,
			Format:        event.FormatHTML,
			Body:          "revoke message",
			FormattedBody: "<del>revoke message</del>",
		},
	}
}

func (mc *MessageConverter) reuploadOctopusAttachment(
	ctx context.Context,
	blob *octopus.BlobData,
	part *PreparedMedia,
) error {
	intent := getIntent(ctx)
	portal := getPortal(ctx)

	part.FillFileName()

	var err error
	part.URL, part.File, err = intent.UploadMedia(ctx, portal.MXID, blob.Binary, part.FileName, part.Info.MimeType)
	if err != nil {
		return fmt.Errorf("%w: %w", bridgev2.ErrMediaReuploadFailed, err)
	}

	return nil
}

func (mc *MessageConverter) makeMediaFailure(ctx context.Context, mediaInfo *PreparedMedia, err error) *bridgev2.ConvertedMessagePart {
	logLevel := zerolog.ErrorLevel
	var extra map[string]any
	var dbMeta any
	errorMsg := fmt.Sprintf("Failed to bridge %s, please view it on the WhatsApp app", mediaInfo.TypeDescription)
	zerolog.Ctx(ctx).WithLevel(logLevel).Err(err).
		Str("media_type", mediaInfo.TypeDescription).
		Msg("Failed to reupload WhatsApp attachment")
	part := &bridgev2.ConvertedMessagePart{
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			MsgType: event.MsgNotice,
			Body:    errorMsg,
		},
		Extra:      extra,
		DBMetadata: dbMeta,
	}
	if mediaInfo.FormattedBody != "" {
		part.Content.EnsureHasHTML()
		part.Content.FormattedBody += "<br><br>" + mediaInfo.FormattedBody
		part.Content.Body += "\n\n" + mediaInfo.Body
	} else if mediaInfo.Body != "" && mediaInfo.FileName != "" && mediaInfo.Body != mediaInfo.FileName {
		part.Content.Body += "\n\n" + mediaInfo.Body
	}
	return part
}

func (mc *MessageConverter) addMentions(ctx context.Context, mentionedID []string, into *event.MessageEventContent) {
	if len(mentionedID) == 0 {
		return
	}

	into.EnsureHasHTML()

	for _, id := range mentionedID {
		if id == "room" {
			into.Mentions.Room = true
			continue
		}

		mxid, displayname, err := mc.getBasicUserInfo(ctx, octopusid.MakeUserID(id))
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Str("id", id).Msg("Failed to get user info")
			continue
		}
		into.Mentions.UserIDs = append(into.Mentions.UserIDs, mxid)
		mentionText := "@" + id
		into.Body = strings.ReplaceAll(into.Body, mentionText, displayname)
		into.FormattedBody = strings.ReplaceAll(into.FormattedBody, mentionText, fmt.Sprintf(`<a href="%s">%s</a>`, mxid.URI().MatrixToURL(), html.EscapeString(displayname)))
	}
}

func (mc *MessageConverter) getBasicUserInfo(ctx context.Context, user networkid.UserID) (id.UserID, string, error) {
	ghost, err := mc.Bridge.GetGhostByID(ctx, user)
	if err != nil {
		return "", "", fmt.Errorf("failed to get ghost by ID: %w", err)
	}
	login := mc.Bridge.GetCachedUserLoginByID(networkid.UserLoginID(user))
	if login != nil {
		return login.UserMXID, ghost.Name, nil
	}
	return ghost.Intent.GetMXID(), ghost.Name, nil
}

func prepareMediaMessage(msgType octopus.MessageType, blob *octopus.BlobData) *PreparedMedia {
	extraInfo := map[string]any{}
	data := &PreparedMedia{
		Type: event.EventMessage,
		MessageEventContent: &event.MessageEventContent{
			Info: &event.FileInfo{},
		},
		Extra: map[string]any{
			"info": extraInfo,
		},
	}

	switch msgType {
	case octopus.MsgImage:
		data.MsgType = event.MsgImage
		data.FileName = "image" + mimetype.Lookup(blob.Mime).Extension()
	case octopus.MsgSticker:
		data.MsgType = event.MsgImage
		data.FileName = "image" + mimetype.Lookup(blob.Mime).Extension()
	case octopus.MsgAudio:
		data.MsgType = event.MsgAudio
		data.FileName = "audio" + mimetype.Lookup(blob.Mime).Extension()
		if blob.Mime == "audio/ogg" {
			data.MSC3245Voice = &event.MSC3245Voice{}
			data.FileName = "Voice message.ogg"
		}
	case octopus.MsgVideo:
		data.MsgType = event.MsgVideo
		data.FileName = "video" + mimetype.Lookup(blob.Mime).Extension()
	case octopus.MsgFile:
		data.MsgType = event.MsgFile
		data.FileName = blob.Name
	}

	data.Body = data.FileName

	data.Info.Size = len(blob.Binary)
	data.Info.MimeType = blob.Mime

	return data
}

func getClient(ctx context.Context) *octopus.OctopusClient {
	return ctx.Value(contextKeyClient).(*octopus.OctopusClient)
}

func getIntent(ctx context.Context) bridgev2.MatrixAPI {
	return ctx.Value(contextKeyIntent).(bridgev2.MatrixAPI)
}

func getPortal(ctx context.Context) *bridgev2.Portal {
	return ctx.Value(contextKeyPortal).(*bridgev2.Portal)
}
