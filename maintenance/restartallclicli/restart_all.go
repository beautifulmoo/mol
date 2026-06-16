// Package restartallclicli implements `agent --restart-all-remotes` (UDP discovery + POST restart-all).
package restartallclicli

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discoverycli"
)

// Run parses flags and restarts all remotes found by discovery.
//
//	<bin> agent --restart-all-remotes [-apiprefix <path>] [-maintenance-port=N]
func Run(args []string) int {
	fs := flag.NewFlagSet("restart-all-remotes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apiPrefixFlag := fs.String("apiprefix", "", fmt.Sprintf("maintenance API path prefix (default %s)", clirest.DefaultAPIPrefix))
	maintenancePortFlag := fs.Int("maintenance-port", clirest.DefaultMaintenancePort, "local maintenance HTTP port (orchestrator)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --restart-all-remotes [-apiprefix=<path>] [-maintenance-port=N]\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Runs UDP discovery (default ports/timeouts), then POST {APIPrefix}/service-control/restart-all\n")
		fmt.Fprintf(os.Stderr, "  on the local maintenance HTTP listener with discovered remote hosts.\n")
		fmt.Fprintf(os.Stderr, "  The orchestrator maintenance service must be running (%s -cfg <file>).\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Discovery is built in (dest-port %d, src-port %d, timeout %ds, service %q); no separate\n",
			discoverycli.DefaultDestPort, discoverycli.DefaultSrcPort, discoverycli.DefaultTimeoutSec, "Mole-Discovery")
		fmt.Fprintf(os.Stderr, "  --discovery run is required or used as input.\n")
		fmt.Fprintf(os.Stderr, "  Discovery flags (--dest-port, --src-port, --timeout, --service) are not on this command.\n")
		fmt.Fprintf(os.Stderr, "  To probe with custom discovery settings only, use %s agent --discovery (standalone).\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  -apiprefix=<path>\n        API path prefix (default %s)\n", clirest.DefaultAPIPrefix)
		fmt.Fprintf(os.Stderr, "  -maintenance-port=N\n        local maintenance HTTP port (default %d)\n", clirest.DefaultMaintenancePort)
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
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected arguments (this command takes no positional args)\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}
	if *maintenancePortFlag <= 0 || *maintenancePortFlag > 65535 {
		fmt.Fprintf(os.Stderr, "%s: -maintenance-port must be 1..65535\n", appmeta.BinaryName)
		return 1
	}

	list, err := discoverycli.RunDefaultDiscoveryToStdout()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: discovery: %v\n", appmeta.BinaryName, err)
		return 1
	}

	hosts := discoverycli.BulkPushHostsFromDiscovery(list)
	if len(hosts) == 0 {
		fmt.Println("No remote hosts found; nothing to restart.")
		return 1
	}
	fmt.Printf("Found %d remote host(s) for restart.\n", len(hosts))

	checkClient := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(checkClient, *apiPrefixFlag, *maintenancePortFlag); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	fmt.Printf("Restarting %d remote host(s)...\n", len(hosts))

	var succeeded, failed int
	var doneTotal int
	streamClient := clirest.DefaultHTTPClient(0)
	err = clirest.RestartAllRemotes(streamClient, *apiPrefixFlag, *maintenancePortFlag, hosts, func(evt map[string]interface{}) error {
		typ, _ := evt["type"].(string)
		switch typ {
		case "start":
			if t, ok := evt["total"].(float64); ok {
				doneTotal = int(t)
			}
		case "progress":
			status, _ := evt["status"].(string)
			ip, _ := evt["ip"].(string)
			hostname, _ := evt["hostname"].(string)
			label := clirest.FormatBulkHostLabel(hostname, ip)
			cur, _ := evt["current"].(float64)
			tot, _ := evt["total"].(float64)
			prefix := fmt.Sprintf("[%d/%d] %s: ", int(cur), int(tot), label)
			switch status {
			case "success":
				succeeded++
				suffix := ""
				if connectIP, _ := evt["connect_ip"].(string); strings.TrimSpace(connectIP) != "" {
					suffix = " via " + strings.TrimSpace(connectIP)
				}
				detail := ""
				if vd, _ := evt["verify_detail"].(string); strings.TrimSpace(vd) != "" {
					detail = " — " + strings.TrimSpace(vd)
				}
				fmt.Println(prefix + "restart verified" + suffix + detail)
			case "fail":
				failed++
				msg, _ := evt["message"].(string)
				if strings.TrimSpace(msg) == "" {
					if vd, _ := evt["verify_detail"].(string); strings.TrimSpace(vd) != "" {
						msg = strings.TrimSpace(vd)
					} else {
						msg = "unknown error"
					}
				}
				fmt.Println(prefix + "fail — " + msg)
			default:
				fmt.Println(prefix + status)
			}
		case "done":
			if s, ok := evt["succeeded"].(float64); ok {
				succeeded = int(s)
			}
			if f, ok := evt["failed"].(float64); ok {
				failed = int(f)
			}
			if t, ok := evt["total"].(float64); ok {
				doneTotal = int(t)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: restart-all: %v\n", appmeta.BinaryName, err)
		return 1
	}

	if doneTotal == 0 {
		fmt.Println("No hosts were processed.")
		return 1
	}
	if failed > 0 {
		fmt.Printf("Done: succeeded=%d failed=%d (total %d).\n", succeeded, failed, doneTotal)
		return 1
	}
	fmt.Printf("Done: all %d host(s) restarted successfully.\n", succeeded)
	return 0
}
