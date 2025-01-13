package connector

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/duo/matrix-octopus/pkg/octopus"

	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

type resyncQueueItem struct {
	portal *bridgev2.Portal
	ghost  *bridgev2.Ghost
}

type OctopusClient struct {
	Main      *OctopusConnector
	UserLogin *bridgev2.UserLogin
	Client    *octopus.OctopusClient

	stopLoops       atomic.Pointer[context.CancelFunc]
	resyncQueue     map[string]resyncQueueItem
	resyncQueueLock sync.Mutex
	nextResync      time.Time

	ID string
}

var (
	_ bridgev2.NetworkAPI                    = (*OctopusClient)(nil)
	_ bridgev2.IdentifierResolvingNetworkAPI = (*OctopusClient)(nil)
)

func (oc *OctopusClient) Connect(ctx context.Context) {
	if oc.Client == nil {
		oc.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Message:    "You're not logged into Octopus",
		})
		return
	}

	// Start sync
	ctx, cancel := context.WithCancel(context.Background())
	oldStop := oc.stopLoops.Swap(&cancel)
	if oldStop != nil {
		(*oldStop)()
	}
	go oc.ghostResyncLoop(ctx)

	go func() {
		for {
			//if err := oc.Client.Connect(oc.ID); err == nil {
			if err := oc.Client.Connect(); err == nil {
				oc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
				break
			} else {
				if _, ok := err.(*octopus.NoAgentError); !ok {
					break
				}
			}

			time.Sleep(time.Second)
		}
	}()
}

func (oc *OctopusClient) Disconnect() {
	// Stop sync
	if stopSyncLoop := oc.stopLoops.Swap(nil); stopSyncLoop != nil {
		(*stopSyncLoop)()
	}

	if cli := oc.Client; cli != nil {
		cli.Disconnect()
		oc.Client = nil
	}
}

func (oc *OctopusClient) LogoutRemote(ctx context.Context) {
	// TODO: client logout remote
}

func (oc *OctopusClient) IsLoggedIn() bool {
	return oc.Client != nil && oc.Client.IsLoggedIn()
}

func (oc *OctopusClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *bridgev2.NetworkRoomCapabilities {
	return &bridgev2.NetworkRoomCapabilities{
		UserMentions:     true,
		LocationMessages: true,
		Replies:          true,
		Deletes:          true,
	}
}

func (oc *OctopusClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return networkid.UserLoginID(userID) == oc.UserLogin.ID
}
