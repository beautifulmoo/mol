package main

import (
	"os"

	"mol/maintenance"
)

// Version is set at build time: -ldflags "-X main.Version=1.2.3"
var Version string

func main() {
	os.Exit(maintenance.Run(Version, os.Args))
}
