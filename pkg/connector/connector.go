package connector

import (
	"context"

	"github.com/duo/matrix-octopus/pkg/msgconv"
	"github.com/duo/matrix-octopus/pkg/octopus"

	"maunium.net/go/mautrix/bridgev2"
)

var (
	_ bridgev2.NetworkConnector      = (*OctopusConnector)(nil)
	_ bridgev2.MaxFileSizeingNetwork = (*OctopusConnector)(nil)
	_ bridgev2.StoppableNetwork      = (*OctopusConnector)(nil)
)

type OctopusConnector struct {
	Service *octopus.OcotpusService
	MsgConv *msgconv.MessageConverter
	Bridge  *bridgev2.Bridge
	Config  Config
}

func (oc *OctopusConnector) Init(bridge *bridgev2.Bridge) {
	// TODO:
	oc.Service = octopus.NewOctopusService(
		bridge.Log,
		oc.Config.ListenAddress,
		oc.Config.ListenSecret,
	)
	oc.MsgConv = msgconv.New(bridge)
	oc.Bridge = bridge
}

func (oc *OctopusConnector) Start(ctx context.Context) error {
	go oc.Service.Start()

	return nil
}

func (oc *OctopusConnector) Stop() {
	oc.Service.Stop()
}

func (oc *OctopusConnector) SetMaxFileSize(maxSize int64) {
	oc.MsgConv.MaxFileSize = maxSize
}

func (oc *OctopusConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{}
}

func (oc *OctopusConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:      "Matrix Octopus",
		NetworkURL:       "https://github.com/duo/matrix-octopus",
		NetworkIcon:      "mxc://matrix.org/dGvOTCfuaMzurkszbDwbksMY",
		NetworkID:        "octopus",
		BeeperBridgeType: "github.com/duo/matrix-octopus",
		DefaultPort:      27777,
	}
}

func (oc *OctopusConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	client := &OctopusClient{
		Main:        oc,
		UserLogin:   login,
		resyncQueue: make(map[string]resyncQueueItem),
	}
	client.Client = oc.Service.NewClient(string(login.UserMXID), client.handleOctopusMessage, client.dispose)

	client.ID = string(login.ID)

	login.Client = client

	return nil
}
