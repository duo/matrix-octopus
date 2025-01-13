package octopusid

import (
	"github.com/duo/matrix-octopus/pkg/octopus"

	"go.mau.fi/util/jsontime"
)

type GhostMetadata struct {
	LastSync jsontime.Unix `json:"last_sync,omitempty"`
}

type PortalMetadata struct {
	ChatType octopus.ChatType `json:"chat_type"`
	LastSync jsontime.Unix    `json:"last_sync,omitempty"`
}
