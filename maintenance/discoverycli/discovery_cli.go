package discoverycli

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
)

// Run runs standalone UDP discovery (no config file, no HTTP server).
// Invoked as: <binary> agent --discovery [--dest-port=N] [--src-port=N] [--timeout=N] [--service=name] (binary name is appmeta.BinaryName).
// Returns 0 on success, 1 on error.
func Run(args []string) int {
	fs := flag.NewFlagSet("discovery", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	destPort := fs.Int("dest-port", DefaultDestPort, "destination UDP port (remote agents listen here)")
	srcPort := fs.Int("src-port", DefaultSrcPort, "local UDP port to bind (responses arrive here)")
	timeoutSec := fs.Int("timeout", DefaultTimeoutSec, "discovery duration in seconds")
	serviceName := fs.String("service", agentcfg.DefaultDiscoveryServiceName, "service name in DISCOVERY_REQUEST")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --discovery [flags]\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Sends DISCOVERY_REQUEST to broadcast:<dest-port>, listens on <src-port>.\n")
		fmt.Fprintf(os.Stderr, "  Each line: [Local|Remote] hostname - primary : [response IPs] version=<key> (<control|compute> if known)\n\n")
		fs.PrintDefaults()
	}
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fs.Usage()
			return 0
		}
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *destPort <= 0 || *destPort > 65535 {
		fmt.Fprintf(os.Stderr, "%s: --dest-port must be 1..65535\n", appmeta.BinaryName)
		return 1
	}
	if *srcPort <= 0 || *srcPort > 65535 {
		fmt.Fprintf(os.Stderr, "%s: --src-port must be 1..65535\n", appmeta.BinaryName)
		return 1
	}
	if *timeoutSec <= 0 {
		fmt.Fprintf(os.Stderr, "%s: --timeout must be positive\n", appmeta.BinaryName)
		return 1
	}

	fmt.Println("Discovery brd (broadcast addresses):")
	for _, brd := range BroadcastAddresses() {
		fmt.Printf("  %s\n", brd)
	}

	nw := len(strconv.Itoa(*timeoutSec))
	if nw < 2 {
		nw = 2
	}
	maxLineLen := len(fmt.Sprintf("Discovering ... %*d ", nw, *timeoutSec))

	list, err := Discover(DiscoverOptions{
		DestPort:    *destPort,
		SrcPort:     *srcPort,
		TimeoutSec:  *timeoutSec,
		ServiceName: *serviceName,
		Progress: func(remaining int) {
			fmt.Printf("\rDiscovering ... %*d ", nw, remaining)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	doneLine := "Discovery Done."
	if len(doneLine) < maxLineLen {
		doneLine = doneLine + strings.Repeat(" ", maxLineLen-len(doneLine))
	}
	fmt.Printf("\r%s\n", doneLine)

	lines := formatResults(list)
	for _, line := range lines {
		fmt.Println(line)
	}
	return 0
}
