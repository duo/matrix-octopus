package main

import (
	"github.com/duo/matrix-octopus/pkg/connector"

	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	m := mxmain.BridgeMain{
		Name:        "mautrix-octopus",
		URL:         "https://github.com/duo/matrix-octopus",
		Description: "A Matrix-Octopus puppeting bridge.",
		Version:     "0.0.1",
		Connector:   &connector.OctopusConnector{},
	}

	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
