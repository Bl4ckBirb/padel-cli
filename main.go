package main

import (
	// Embed the IANA timezone database into the binary. Without this,
	// time.LoadLocation depends on the host having zoneinfo (via a Go install,
	// ZONEINFO, or OS files) — which Windows and minimal containers lack. When
	// it's missing, venue timezone lookups silently fall back to UTC and all
	// displayed times are wrong. Embedding makes conversions work everywhere.
	_ "time/tzdata"

	"padel-cli/cmd"
)

func main() {
	cmd.Execute()
}
