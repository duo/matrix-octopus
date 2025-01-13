package connector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/duo/matrix-octopus/pkg/octopus"
	"github.com/duo/matrix-octopus/pkg/octopusid"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

const (
	LoginStepQR       = "me.lxduo.octopus.login.qr"
	LoginStepWait     = "me.lxduo.octopus.login.wait"
	LoginStepComplete = "me.lxduo.octopus.login.complete"
)

type QRLogin struct {
	User   *bridgev2.User
	Main   *OctopusConnector
	Client *octopus.OctopusClient
	Log    zerolog.Logger
}

var (
	qrTimeout = 3 * time.Minute

	_ bridgev2.LoginProcessDisplayAndWait = (*QRLogin)(nil)
)

func (oc *OctopusConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "QR",
		Description: "Scan a QR code to pair the bridge to your Octopus client",
		ID:          "qr",
	}}
}

func (oc *OctopusConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	if flowID != "qr" {
		return nil, fmt.Errorf("invalid login flow ID")
	}
	return &QRLogin{
		User: user,
		Main: oc,
		Log: user.Log.With().
			Str("action", "login").
			Stringer("user_id", user.MXID).
			Logger(),
	}, nil
}

func (qr *QRLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	qr.Client = qr.Main.Service.NewClient(string(qr.User.MXID), nil, nil)

	if err := qr.Client.Connect(); err != nil {
		qr.Log.Warn().Err(err).Msg("Failed to connect to Octopus")
		return nil, err
	}

	step, err := qr.Client.Login(&octopus.LoginStep{
		StepID: LoginStepQR,
		Type:   octopus.LoginStepTypeDisplayAndWait,
		DisplayAndWaitParams: &octopus.LoginDisplayAndWaitParams{
			Type: octopus.LoginDisplayTypeQR,
		},
	})
	if err != nil {
		qr.Log.Warn().Err(err).Msg("Failed to continue login step from Octopus")
		return nil, err
	}

	switch step.Type {
	case octopus.LoginStepTypeDisplayAndWait:
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeDisplayAndWait,
			StepID:       step.StepID,
			Instructions: step.Instructions,
			DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
				Type: bridgev2.LoginDisplayTypeQR, // TODO:
				Data: step.DisplayAndWaitParams.Data,
			},
		}, nil
	case octopus.LoginStepTypeWait:
		return qr.Wait(ctx)
	case octopus.LoginStepTypeComplete:
		return qr.completeStep(ctx, step)
	default:
		return nil, fmt.Errorf("step %s not supported", step.Type)
	}
}

func (qr *QRLogin) Wait(ctx context.Context) (*bridgev2.LoginStep, error) {
	ctxTimeout, cancel := context.WithTimeout(ctx, qrTimeout)
	defer cancel()

	for {
		step, err := qr.Client.Login(&octopus.LoginStep{
			StepID: LoginStepWait,
			Type:   octopus.LoginStepTypeWait,
		})
		if err == nil && step.Type == octopus.LoginStepTypeComplete {
			if ret, err := qr.completeStep(ctx, step); err == nil {
				return ret, err
			}
		}

		select {
		case <-time.After(3 * time.Second):
		case <-ctxTimeout.Done():
			return nil, errors.New("timed out waiting for login")
		}
	}
}

func (qr *QRLogin) Cancel() {
	qr.Client.Disconnect()
}

func (qr *QRLogin) completeStep(ctx context.Context, step *octopus.LoginStep) (*bridgev2.LoginStep, error) {
	loginInfo, err := qr.Client.GetLoginInfo()
	if err == nil && loginInfo != nil {
		newLoginID := octopusid.MakeUserLoginID(loginInfo.ID)
		ul, err := qr.User.NewLogin(ctx, &database.UserLogin{
			ID:         newLoginID,
			RemoteName: loginInfo.Name,
		}, &bridgev2.NewLoginParams{
			DeleteOnConflict: true,
		})

		if err != nil {
			return nil, fmt.Errorf("failed to create user login: %w", err)
		}

		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeComplete,
			StepID:       LoginStepComplete,
			Instructions: step.Instructions,
			CompleteParams: &bridgev2.LoginCompleteParams{
				UserLoginID: ul.ID,
				UserLogin:   ul,
			},
		}, nil
	}

	return nil, fmt.Errorf("failed to complete login: %w", err)
}
