package connector

import (
	"github.com/duo/matrix-octopus/pkg/octopusid"

	"maunium.net/go/mautrix/bridgev2/database"
)

func (tc *OctopusConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		Portal: func() any {
			return &octopusid.PortalMetadata{}
		},
		Ghost: func() any {
			return &octopusid.GhostMetadata{}
		},
		Message:   nil,
		Reaction:  nil,
		UserLogin: nil,
	}
}
