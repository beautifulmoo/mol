package discoverycli

import (
	"fmt"
	"strconv"
	"strings"

	"contrabass-agent/maintenance/discovery"
)

// RunDefaultDiscoveryToStdout prints brd addresses and a countdown, then runs Discover with default options.
func RunDefaultDiscoveryToStdout() ([]discovery.DiscoveryResponse, error) {
	fmt.Println("Discovery brd (broadcast addresses):")
	for _, brd := range BroadcastAddresses() {
		fmt.Printf("  %s\n", brd)
	}

	timeoutSec := DefaultTimeoutSec
	nw := len(strconv.Itoa(timeoutSec))
	if nw < 2 {
		nw = 2
	}
	maxLineLen := len(fmt.Sprintf("Discovering ... %*d ", nw, timeoutSec))

	list, err := Discover(DiscoverOptions{
		DestPort:    DefaultDestPort,
		SrcPort:     DefaultSrcPort,
		TimeoutSec:  timeoutSec,
		ServiceName: "",
		Progress: func(remaining int) {
			fmt.Printf("\rDiscovering ... %*d ", nw, remaining)
		},
	})
	if err != nil {
		return nil, err
	}

	doneLine := "Discovery Done."
	if len(doneLine) < maxLineLen {
		doneLine = doneLine + strings.Repeat(" ", maxLineLen-len(doneLine))
	}
	fmt.Printf("\r%s\n", doneLine)
	return list, nil
}
