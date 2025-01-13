package connector

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"path"
	"time"

	"github.com/duo/matrix-octopus/pkg/octopus"
	"github.com/duo/matrix-octopus/pkg/octopusid"

	"github.com/rs/zerolog"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
)

const (
	PrivateChatTopic = "Octopus private chat"

	powerDefault    = 0
	powerAdmin      = 50
	powerSuperAdmin = 75

	resyncMinInterval  = 7 * 24 * time.Hour
	resyncLoopInterval = 4 * time.Hour
)

func (oc *OctopusClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	meta := portal.Metadata.(*octopusid.PortalMetadata)
	id := string(portal.ID)

	switch meta.ChatType {
	case octopus.ChatPrivate:
		return oc.makeDirectChatInfo(id), nil
	case octopus.ChatGroup:
		if info, err := oc.Client.GetGroupInfo(id); err != nil {
			return nil, err
		} else {
			wrapped := oc.wrapGroupChatInfo(info)
			wrapped.ExtraUpdates = bridgev2.MergeExtraUpdaters(wrapped.ExtraUpdates, updatePortalLastSyncAt)
			return wrapped, nil
		}
	}

	return nil, fmt.Errorf("unknown chat type")
}

func (oc *OctopusClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	if ghost.Name != "" {
		oc.EnqueueGhostResync(ghost)
		return nil, nil
	}

	if contact, err := oc.Client.GetUserInfo(string(ghost.ID)); err != nil {
		return nil, err
	} else {
		return oc.contactToUserInfo(contact), nil
	}
}

func (oc *OctopusClient) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	ghost, err := oc.Main.Bridge.GetGhostByID(ctx, octopusid.MakeUserID(identifier))
	if err != nil {
		return nil, fmt.Errorf("failed to get ghost: %w", err)
	}

	return &bridgev2.ResolveIdentifierResponse{
		Ghost:  ghost,
		UserID: octopusid.MakeUserID(identifier),
		Chat:   &bridgev2.CreateChatResponse{PortalKey: oc.makeDMPortalKey(identifier)},
	}, nil
}

func (oc *OctopusClient) EnqueueGhostResync(ghost *bridgev2.Ghost) {
	if ghost.Metadata.(*octopusid.GhostMetadata).LastSync.Add(resyncMinInterval).After(time.Now()) {
		return
	}

	oc.resyncQueueLock.Lock()
	uid := fmt.Sprintf("u\u0001%s", string(ghost.ID))
	if _, exists := oc.resyncQueue[uid]; !exists {
		oc.resyncQueue[uid] = resyncQueueItem{ghost: ghost}
		oc.UserLogin.Log.Debug().
			Str("uid", uid).
			Stringer("next_resync_in", time.Until(oc.nextResync)).
			Msg("Enqueued resync for ghost")
	}
	oc.resyncQueueLock.Unlock()
}

func (oc *OctopusClient) EnqueuePortalResync(portal *bridgev2.Portal) {
	meta := portal.Metadata.(*octopusid.PortalMetadata)
	if meta.ChatType != octopus.ChatGroup || meta.LastSync.Add(resyncMinInterval).After(time.Now()) {
		return
	}

	oc.resyncQueueLock.Lock()
	uid := fmt.Sprintf("g\u0001%s", string(portal.ID))
	if _, exists := oc.resyncQueue[uid]; !exists {
		oc.resyncQueue[uid] = resyncQueueItem{portal: portal}
		oc.UserLogin.Log.Debug().
			Str("uid", uid).
			Stringer("next_resync_in", time.Until(oc.nextResync)).
			Msg("Enqueued resync for portal")
	}
	oc.resyncQueueLock.Unlock()
}

func (oc *OctopusClient) ghostResyncLoop(ctx context.Context) {
	log := oc.UserLogin.Log.With().Str("action", "ghost resync loop").Logger()
	ctx = log.WithContext(ctx)
	oc.nextResync = time.Now().Add(resyncLoopInterval).Add(-time.Duration(rand.IntN(60)) * time.Second)
	timer := time.NewTimer(time.Until(oc.nextResync))
	log.Info().Time("first_resync", oc.nextResync).Msg("Ghost resync queue starting")
	for {
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		queue := oc.rotateResyncQueue()
		timer.Reset(time.Until(oc.nextResync))
		if len(queue) > 0 {
			oc.doGhostResync(ctx, queue)
		} else {
			log.Trace().Msg("Nothing in background resync queue")
		}
	}
}

func (oc *OctopusClient) rotateResyncQueue() map[string]resyncQueueItem {
	oc.resyncQueueLock.Lock()
	defer oc.resyncQueueLock.Unlock()
	oc.nextResync = time.Now().Add(resyncLoopInterval)
	if len(oc.resyncQueue) == 0 {
		return nil
	}
	queue := oc.resyncQueue
	oc.resyncQueue = make(map[string]resyncQueueItem)
	return queue
}

func (oc *OctopusClient) doGhostResync(ctx context.Context, queue map[string]resyncQueueItem) {
	log := zerolog.Ctx(ctx)
	if !oc.IsLoggedIn() {
		log.Warn().Msg("Not logged in, skipping background resyncs")
		return
	}

	log.Debug().Msg("Starting background resyncs")
	defer log.Debug().Msg("Background resyncs finished")

	var ghosts []*bridgev2.Ghost
	var portals []*bridgev2.Portal

	for uid, item := range queue {
		var lastSync time.Time
		if item.ghost != nil {
			lastSync = item.ghost.Metadata.(*octopusid.GhostMetadata).LastSync.Time
		} else if item.portal != nil {
			lastSync = item.portal.Metadata.(*octopusid.PortalMetadata).LastSync.Time
		}
		if lastSync.Add(resyncMinInterval).After(time.Now()) {
			log.Debug().
				Str("uid", uid).
				Time("last_sync", lastSync).
				Msg("Not resyncing, last sync was too recent")
			continue
		}
		if item.ghost != nil {
			ghosts = append(ghosts, item.ghost)
		} else if item.portal != nil {
			portals = append(portals, item.portal)
		}
	}

	for _, portal := range portals {
		oc.Main.Bridge.QueueRemoteEvent(oc.UserLogin, &simplevent.ChatResync{
			EventMeta: simplevent.EventMeta{
				Type: bridgev2.RemoteEventChatResync,
				LogContext: func(c zerolog.Context) zerolog.Context {
					return c.Str("sync_reason", "queue")
				},
				PortalKey: portal.PortalKey,
			},
			GetChatInfoFunc: func(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
				info, err := oc.GetChatInfo(ctx, portal)
				if err == nil {
					info.ExtraUpdates = bridgev2.MergeExtraUpdaters(
						info.ExtraUpdates,
						func(ctx context.Context, p *bridgev2.Portal) bool {
							oc.updateMemberDisplyname(ctx, p, string(portal.ID))
							return true
						})
				}
				return info, err
			},
		})
	}

	for _, ghost := range ghosts {
		id := string(ghost.ID)
		contact, err := oc.Client.GetUserInfo(id)
		if err != nil {
			log.Warn().Str("id", id).Msg("Failed to get user info for puppet in background sync")
			continue
		}
		ghost.UpdateInfo(ctx, oc.contactToUserInfo(contact))
	}
}

func (oc *OctopusClient) makeDirectChatInfo(recipient string) *bridgev2.ChatInfo {
	members := &bridgev2.ChatMemberList{
		IsFull:           true,
		TotalMemberCount: 2,
		OtherUserID:      octopusid.MakeUserID(recipient),
		PowerLevels:      nil,
	}

	if networkid.UserLoginID(recipient) != oc.UserLogin.ID {
		selfEvtSender := oc.selfEventSender()
		members.MemberMap = map[networkid.UserID]bridgev2.ChatMember{
			selfEvtSender.Sender: {EventSender: selfEvtSender},
			members.OtherUserID:  {EventSender: oc.makeEventSender(recipient)},
		}
	} else {
		members.MemberMap = map[networkid.UserID]bridgev2.ChatMember{
			// For chats with self, force-split the members so the user's own ghost is always in the room.
			"":                  {EventSender: bridgev2.EventSender{IsFromMe: true}},
			members.OtherUserID: {EventSender: bridgev2.EventSender{Sender: members.OtherUserID}},
		}
	}

	return &bridgev2.ChatInfo{
		Topic:        ptr.Ptr(PrivateChatTopic),
		Members:      members,
		Type:         ptr.Ptr(database.RoomTypeDM),
		ExtraUpdates: updateChatType(octopus.ChatPrivate),
	}
}

func (oc *OctopusClient) wrapGroupChatInfo(info *octopus.GroupInfo) *bridgev2.ChatInfo {
	wrapped := &bridgev2.ChatInfo{
		Name:   ptr.Ptr(info.Name),
		Topic:  ptr.Ptr(info.Notice),
		Avatar: wrapAvatar(info.Avatar),
		Members: &bridgev2.ChatMemberList{
			IsFull:           true,
			TotalMemberCount: len(info.Members),
			MemberMap:        make(map[networkid.UserID]bridgev2.ChatMember, len(info.Members)),
			PowerLevels: &bridgev2.PowerLevelOverrides{
				Events: map[event.Type]int{
					event.StateRoomName:   powerDefault,
					event.StateRoomAvatar: powerDefault,
					event.StateTopic:      powerDefault,
					event.EventReaction:   powerDefault,
					event.EventRedaction:  powerDefault,
				},
				EventsDefault: ptr.Ptr(powerDefault),
				StateDefault:  ptr.Ptr(powerAdmin),
			},
		},
		Disappear:    &database.DisappearingSetting{Type: database.DisappearingTypeNone},
		Type:         ptr.Ptr(database.RoomTypeDefault),
		ExtraUpdates: updateChatType(octopus.ChatGroup),
	}

	for _, member := range info.Members {
		evtSender := oc.makeEventSender(member)
		wrapped.Members.MemberMap[evtSender.Sender] = bridgev2.ChatMember{
			EventSender: evtSender,
			Membership:  event.MembershipJoin,
			PowerLevel:  ptr.Ptr(powerDefault),
		}
	}

	return wrapped
}

func (oc *OctopusClient) contactToUserInfo(contact *octopus.UserInfo) *bridgev2.UserInfo {
	ui := &bridgev2.UserInfo{
		IsBot:        nil,
		Identifiers:  []string{},
		ExtraUpdates: updateGhostLastSyncAt,
		Name: ptr.Ptr(oc.Main.Config.FormatDisplayname(DisplaynameParams{
			Alias: contact.Alias,
			Name:  contact.Name,
			ID:    contact.ID,
		})),
		Avatar: wrapAvatar(contact.Avatar),
	}

	return ui
}

func updateChatType(chatType octopus.ChatType) func(ctx context.Context, portal *bridgev2.Portal) bool {
	return func(ctx context.Context, portal *bridgev2.Portal) (changed bool) {
		meta := portal.Metadata.(*octopusid.PortalMetadata)
		if meta.ChatType != chatType {
			meta.ChatType = chatType
			changed = true
		}

		return
	}
}

//	func (oc *OctopusClient) updateMemberDisplyname(groupID string) func(ctx context.Context, portal *bridgev2.Portal) bool {
//		return func(ctx context.Context, portal *bridgev2.Portal) (changed bool) {
func (oc *OctopusClient) updateMemberDisplyname(ctx context.Context, portal *bridgev2.Portal, groupID string) {
	if memberList, err := oc.Client.GetGroupMemberList(groupID); err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to get group member list")
	} else {
		for _, member := range memberList {
			memberIntent := portal.GetIntentFor(ctx, oc.makeEventSender(member.ID), oc.UserLogin, bridgev2.RemoteEventChatInfoChange)

			mxid := memberIntent.GetMXID()

			memberInfo, err := portal.Bridge.Matrix.GetMemberInfo(ctx, portal.MXID, mxid)
			if err != nil {
				zerolog.Ctx(ctx).Err(err).Msg("Failed to get member info")
				continue
			}

			displayName := cmp.Or(member.Alias, member.Name)
			if memberInfo.Displayname != displayName {
				memberInfo.Displayname = displayName

				var zeroTime time.Time
				_, err = memberIntent.SendState(ctx, portal.MXID, event.StateMember, mxid.String(), &event.Content{
					Parsed: memberInfo,
				}, zeroTime)

				if err != nil {
					zerolog.Ctx(ctx).Err(err).Stringer("user_id", mxid).Msg("Failed to update group displayname")
				}
				zerolog.Ctx(ctx).Debug().Stringer("user_id", mxid).Msgf("Update group displayname to %s", displayName)
			}
		}
	}

	return
}

//}

func updateGhostLastSyncAt(ctx context.Context, ghost *bridgev2.Ghost) bool {
	meta := ghost.Metadata.(*octopusid.GhostMetadata)
	forceSave := time.Since(meta.LastSync.Time) > 24*time.Hour
	meta.LastSync = jsontime.UnixNow()
	return forceSave
}

func updatePortalLastSyncAt(_ context.Context, portal *bridgev2.Portal) bool {
	meta := portal.Metadata.(*octopusid.PortalMetadata)
	forceSave := time.Since(meta.LastSync.Time) > 24*time.Hour
	meta.LastSync = jsontime.UnixNow()
	return forceSave
}

func wrapAvatar(avatarURL string) *bridgev2.Avatar {
	if avatarURL == "" {
		return &bridgev2.Avatar{Remove: true}
	}
	parsedURL, _ := url.Parse(avatarURL)
	avatarID := path.Base(parsedURL.Path)
	return &bridgev2.Avatar{
		ID: networkid.AvatarID(avatarID),
		Get: func(ctx context.Context) ([]byte, error) {
			return GetBytes(avatarURL)
		},
	}
}
