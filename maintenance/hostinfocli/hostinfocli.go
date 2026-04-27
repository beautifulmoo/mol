// Package hostinfocli implements --host-info CLI using shared hostinfoapi logic (no local maintenance HTTP required).
package hostinfocli

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"contrabass-agent/maintenance/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfoapi"
)

const defaultHostInfoSrcUDP = 9998

// Run runs: <bin> agent --host-info -cfg <config> [flags] <self|remote-ip>
// buildVersionKey is the same ldflags-injected value as main (used for Self VERSION field when target is self).
// Flag/positional order is flexible: e.g. <ip> -cfg path works as well as -cfg path <ip>.
func Run(buildVersionKey string, args []string) int {
	cfgPath, srcPort, pos, showHelp, err := parseHostInfoArgs(args)
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
		fmt.Fprintf(os.Stderr, "%s: expected exactly one argument: <self|remote-ip>\n", appmeta.BinaryName)
		printUsage()
		return 1
	}
	if strings.TrimSpace(cfgPath) == "" {
		fmt.Fprintf(os.Stderr, "%s: -cfg <config.yaml> is required\n", appmeta.BinaryName)
		printUsage()
		return 1
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load config: %v\n", appmeta.BinaryName, err)
		return 1
	}

	target := strings.TrimSpace(pos[0])
	if target == "" {
		fmt.Fprintf(os.Stderr, "%s: target must not be empty\n", appmeta.BinaryName)
		return 1
	}

	displayVersion := strings.TrimSpace(buildVersionKey)
	if displayVersion == "" {
		displayVersion = "0.0.0-0"
	}

	if strings.EqualFold(target, "self") {
		info, err := hostinfoapi.LocalSelfInfo()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: host info: %v\n", appmeta.BinaryName, err)
			return 1
		}
		meta := hostinfoapi.SelfMetaFromConfig(cfg, displayVersion)
		data := hostinfoapi.SelfDiscoveryResponse(info, meta)
		printHostInfo(os.Stdout, "host self (local)", data)
		return 0
	}

	if net.ParseIP(target) == nil {
		fmt.Fprintf(os.Stderr, "%s: remote target must be a valid IP address: %q\n", appmeta.BinaryName, target)
		return 1
	}

	disc, cleanup, err := hostinfoapi.StartEphemeralDiscovery(cfg, displayVersion, srcPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: discovery UDP: %v\n", appmeta.BinaryName, err)
		return 1
	}
	defer cleanup()

	resp, err := hostinfoapi.RemoteHostInfo(disc, target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}
	printHostInfo(os.Stdout, "host "+target+" (unicast discovery)", *resp)
	return 0
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s agent --host-info -cfg <config.yaml> [flags] <self|remote-ip>\n\n", appmeta.BinaryName)
	fmt.Fprintf(os.Stderr, "  Same behavior as GET .../host-info: self → local hostinfo; remote → UDP unicast to <ip>:DiscoveryUDPPort.\n")
	fmt.Fprintf(os.Stderr, "  Flags and <self|remote-ip> can appear in any order. Local maintenance HTTP does not need to be running.\n\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	fmt.Fprintf(os.Stderr, "  -cfg path       path to config file (required)\n")
	fmt.Fprintf(os.Stderr, "  -src-port N     local UDP bind port for remote unicast (default %d; use another port if busy)\n", defaultHostInfoSrcUDP)
	fmt.Fprintf(os.Stderr, "  -h, --help      show this help\n")
}

// parseHostInfoArgs parses -cfg, -src-port, help, and one positional (IP or self). Order-independent.
func parseHostInfoArgs(args []string) (cfgPath string, srcPort int, pos []string, showHelp bool, err error) {
	srcPort = defaultHostInfoSrcUDP
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			showHelp = true
			i++
		case a == "-cfg" || a == "--cfg":
			if i+1 >= len(args) {
				return "", 0, nil, false, fmt.Errorf("-cfg requires a path argument")
			}
			cfgPath = args[i+1]
			i += 2
		case strings.HasPrefix(a, "-cfg="):
			cfgPath = strings.TrimPrefix(a, "-cfg=")
			cfgPath = strings.TrimSpace(cfgPath)
			i++
		case strings.HasPrefix(a, "--cfg="):
			cfgPath = strings.TrimPrefix(a, "--cfg=")
			cfgPath = strings.TrimSpace(cfgPath)
			i++
		case a == "-src-port" || a == "--src-port":
			if i+1 >= len(args) {
				return "", 0, nil, false, fmt.Errorf("-src-port requires a port number")
			}
			p, e := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if e != nil || p < 1 || p > 65535 {
				return "", 0, nil, false, fmt.Errorf("-src-port must be an integer 1..65535")
			}
			srcPort = p
			i += 2
		case strings.HasPrefix(a, "-src-port="):
			p, e := strconv.Atoi(strings.TrimPrefix(a, "-src-port="))
			if e != nil || p < 1 || p > 65535 {
				return "", 0, nil, false, fmt.Errorf("-src-port must be an integer 1..65535")
			}
			srcPort = p
			i++
		case strings.HasPrefix(a, "--src-port="):
			p, e := strconv.Atoi(strings.TrimPrefix(a, "--src-port="))
			if e != nil || p < 1 || p > 65535 {
				return "", 0, nil, false, fmt.Errorf("--src-port must be an integer 1..65535")
			}
			srcPort = p
			i++
		case strings.HasPrefix(a, "-"):
			return "", 0, nil, false, fmt.Errorf("unknown flag %q", a)
		default:
			pos = append(pos, a)
			i++
		}
	}
	return cfgPath, srcPort, pos, showHelp, nil
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
	if d.RequestID != "" {
		row("REQUEST_ID", d.RequestID)
	}
	row("CPU_INFO", d.CPUInfo)
	row("CPU_USAGE_PERCENT", fmt.Sprintf("%.2f", d.CPUUsagePercent))
	row("CPU_UUID", d.CPUUUID)
	row("MEMORY_TOTAL_MB", strconv.FormatUint(d.MemoryTotalMB, 10))
	row("MEMORY_USED_MB", strconv.FormatUint(d.MemoryUsedMB, 10))
	row("MEMORY_USAGE_PERCENT", fmt.Sprintf("%.2f", d.MemoryUsagePercent))
	if d.RespondedFromIP != "" {
		row("RESPONDED_FROM_IP", d.RespondedFromIP)
	}
	if d.IsSelf {
		row("SELF", "true")
	}
	_ = tw.Flush()
}
