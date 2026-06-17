package discoverycli

import (
	"fmt"
	"strconv"
	"strings"

	"contrabass-agent/maintenance/discovery"
)

// DiscoverToStdout prints brd addresses and a countdown, runs UDP discovery, prints "Discovery Done."
func DiscoverToStdout(opts DiscoverOptions) ([]discovery.DiscoveryResponse, error) {
	fmt.Println("Discovery brd (broadcast addresses):")
	for _, brd := range BroadcastAddresses() {
		fmt.Printf("  %s\n", brd)
	}

	timeoutSec := opts.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = DefaultTimeoutSec
	}
	nw := len(strconv.Itoa(timeoutSec))
	if nw < 2 {
		nw = 2
	}
	maxLineLen := len(fmt.Sprintf("Discovering ... %*d ", nw, timeoutSec))

	opts.Progress = func(remaining int) {
		fmt.Printf("\rDiscovering ... %*d ", nw, remaining)
	}
	list, err := Discover(opts)
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

// RunDefaultDiscoveryToStdout runs discovery with default ports/timeout and progress UI.
func RunDefaultDiscoveryToStdout() ([]discovery.DiscoveryResponse, error) {
	return DiscoverToStdout(DiscoverOptions{
		DestPort:   DefaultDestPort,
		SrcPort:    DefaultSrcPort,
		TimeoutSec: DefaultTimeoutSec,
	})
}
