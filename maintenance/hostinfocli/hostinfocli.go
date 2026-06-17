// Package hostinfocli implements --host-info CLI via GET .../self on the target agent HTTP API.
package hostinfocli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discovery"
)

// RunOptions optional inputs (REPL passes discovery cache after `discover`).
type RunOptions struct {
	DiscoveryCache []discovery.DiscoveryResponse
}

// Run runs: <bin> agent --host-info [-apiprefix <path>] <self|local|remote-ip>
func Run(buildVersionKey, cliBuildVariant string, args []string, opts *RunOptions) int {
	_ = buildVersionKey
	_ = cliBuildVariant
	var discoveryCache []discovery.DiscoveryResponse
	if opts != nil {
		discoveryCache = opts.DiscoveryCache
	}

	apiPrefix, pos, showHelp, err := clirest.ParseAPIPrefixFlag(args)
	if showHelp {
		printUsage()
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		printUsage()
		return 1
	}
	if len(pos) != 1 {
		fmt.Fprintf(os.Stderr, "%s: expected exactly one argument: <self|local|remote-ip>\n", appmeta.BinaryName)
		printUsage()
		return 1
	}

	target := strings.TrimSpace(pos[0])
	if err := clirest.ValidateTarget(target); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	client := clirest.DefaultHTTPClient(30 * time.Second)
	if err := clirest.EnsureServiceRunning(client, target, apiPrefix); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	data, err := clirest.GetHostInfo(client, target, apiPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	enrichHostAddresses(&data, target, discoveryCache)

	label := "host self (local)"
	if !clirest.IsLocalTarget(target) {
		label = "host " + target + " (HTTP API)"
	}
	printHostInfo(os.Stdout, label, data)
	return 0
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s agent --host-info [-apiprefix <path>] <self|local|remote-ip>\n\n", appmeta.BinaryName)
	fmt.Fprintf(os.Stderr, "  GET {APIPrefix}/self on the target agent (Gin on port %d).\n", clirest.DefaultHTTPPort)
	fmt.Fprintf(os.Stderr, "  Default -apiprefix: %s\n", clirest.DefaultAPIPrefix)
	fmt.Fprintf(os.Stderr, "  The target agent HTTP service must be running.\n\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	fmt.Fprintf(os.Stderr, "  -apiprefix path   API path prefix (default %s)\n", clirest.DefaultAPIPrefix)
	fmt.Fprintf(os.Stderr, "  -h, --help        show this help\n")
}

func printHostInfo(w io.Writer, label string, d discovery.DiscoveryResponse) {
	fmt.Fprintln(w, label)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(tw, "%s\t%s\n", k, v) }
	row("TYPE", d.Type)
	row("SERVICE", d.Service)
	row("HOSTNAME", d.Hostname)
	row("HOST_IP", d.HostIP)
	if len(d.HostIPs) > 0 {
		row("HOST_IPS", strings.Join(d.HostIPs, ", "))
	}
	row("SERVICE_PORT", strconv.Itoa(d.ServicePort))
	row("VERSION", d.Version)
	row("BUILD_VARIANT", formatBuildVariant(d.BuildVariant))
	if d.RequestID != "" {
		row("REQUEST_ID", d.RequestID)
	}
	row("CPU_INFO", d.CPUInfo)
	row("CPU_USAGE_PERCENT", fmt.Sprintf("%.2f", d.CPUUsagePercent))
	row("CPU_UUID", d.CPUUUID)
	row("MEMORY_TOTAL_MB", strconv.FormatUint(d.MemoryTotalMB, 10))
	row("MEMORY_USED_MB", strconv.FormatUint(d.MemoryUsedMB, 10))
	row("MEMORY_USAGE_PERCENT", fmt.Sprintf("%.2f", d.MemoryUsagePercent))
	if d.IsSelf {
		row("SELF", "true")
	}
	_ = tw.Flush()
}

func formatBuildVariant(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}
