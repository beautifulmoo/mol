// Package applyupdateallclicli implements `agent --apply-update-all-remotes` (upload + discovery + apply-update-all).
package applyupdateallclicli

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

// Run parses flags, uploads a bundle to local staging, discovers remotes, and calls apply-update-all.
//
//	<bin> agent --apply-update-all-remotes [-apiprefix <path>] [-maintenance-port=N] [-agent-variant=control|compute] [-use-bundle-config] <bundle.tar.gz>
func Run(cliBuildVariant string, args []string) int {
	fs := flag.NewFlagSet("apply-update-all-remotes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apiPrefixFlag := fs.String("apiprefix", "", fmt.Sprintf("maintenance API path prefix (default %s)", clirest.DefaultAPIPrefix))
	maintenancePortFlag := fs.Int("maintenance-port", clirest.DefaultMaintenancePort, "local maintenance HTTP port (orchestrator)")
	agentVariantFlag := fs.String("agent-variant", "", "control or compute for all remotes; if omitted, use this CLI binary build_variant (compute if unknown)")
	useBundleConfigFlag := fs.Bool("use-bundle-config", false, "apply config from the bundle instead of reusing each remote's current agent.local.yml")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --apply-update-all-remotes [-apiprefix=<path>] [-maintenance-port=N] [-agent-variant=control|compute] [-use-bundle-config] <bundle.tar.gz>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Uploads the bundle to local staging, runs UDP discovery (default ports/timeouts),\n")
		fmt.Fprintf(os.Stderr, "  then POST {APIPrefix}/apply-update-all on the local maintenance HTTP listener.\n")
		fmt.Fprintf(os.Stderr, "  The orchestrator maintenance service must be running (%s -cfg <file>).\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Discovery is built in (dest-port %d, src-port %d, timeout %ds, service %q); no separate\n",
			discoverycli.DefaultDestPort, discoverycli.DefaultSrcPort, discoverycli.DefaultTimeoutSec, "Mole-Discovery")
		fmt.Fprintf(os.Stderr, "  --discovery run is required or used as input.\n")
		fmt.Fprintf(os.Stderr, "  Discovery flags (--dest-port, --src-port, --timeout, --service) are not on this command.\n")
		fmt.Fprintf(os.Stderr, "  To probe with custom discovery settings only, use %s agent --discovery (standalone).\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  -apiprefix=<path>\n        API path prefix (default %s)\n", clirest.DefaultAPIPrefix)
		fmt.Fprintf(os.Stderr, "  -maintenance-port=N\n        local maintenance HTTP port (default %d)\n", clirest.DefaultMaintenancePort)
		fmt.Fprintf(os.Stderr, "  -agent-variant=control|compute\n        variant applied on every remote; if omitted, match this CLI binary build_variant\n")
		fmt.Fprintf(os.Stderr, "  -use-bundle-config\n        use agent.local.yml from the bundle; default is to reuse each remote's\n")
		fmt.Fprintf(os.Stderr, "        current config (same as --apply-update and the web UI reuse checkbox)\n")
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
	pos := fs.Args()
	if len(pos) != 1 {
		fmt.Fprintf(os.Stderr, "%s: expected one argument: <bundle.tar.gz>\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}
	bundlePath := strings.TrimSpace(pos[0])
	if bundlePath == "" {
		fmt.Fprintf(os.Stderr, "%s: bundle path must not be empty\n", appmeta.BinaryName)
		return 1
	}
	if *maintenancePortFlag <= 0 || *maintenancePortFlag > 65535 {
		fmt.Fprintf(os.Stderr, "%s: -maintenance-port must be 1..65535\n", appmeta.BinaryName)
		return 1
	}

	installedBV := strings.TrimSpace(cliBuildVariant)
	if installedBV == "" {
		installedBV = "compute"
	}
	agentVariant, err := clirest.ResolveAgentVariantForTarget(*agentVariantFlag, installedBV)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}
	reusePreviousConfig := !*useBundleConfigFlag

	checkClient := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(checkClient, *apiPrefixFlag, *maintenancePortFlag); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	fmt.Printf("Uploading bundle %s to local staging...\n", bundlePath)
	uploadClient := clirest.DefaultHTTPClient(300 * time.Second)
	versionKey, err := clirest.UploadBundleMaintenance(uploadClient, *apiPrefixFlag, *maintenancePortFlag, bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: upload: %v\n", appmeta.BinaryName, err)
		return 1
	}
	fmt.Printf("Staged version %s (variant %s, reuse_previous_config=%v).\n", versionKey, agentVariant, reusePreviousConfig)

	list, err := discoverycli.RunDefaultDiscoveryToStdout()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: discovery: %v\n", appmeta.BinaryName, err)
		return 1
	}

	hosts := discoverycli.BulkPushHostsFromDiscovery(list)
	if len(hosts) == 0 {
		fmt.Println("No remote hosts found; nothing to apply.")
		return 1
	}
	fmt.Printf("Found %d remote host(s) for apply-update.\n", len(hosts))
	fmt.Printf("Applying version %s to %d remote host(s)...\n", versionKey, len(hosts))

	var succeeded, failed, skipped int
	var doneTotal int
	streamClient := clirest.DefaultHTTPClient(0)
	err = clirest.ApplyUpdateAll(streamClient, *apiPrefixFlag, *maintenancePortFlag, hosts, clirest.ApplyUpdateAllOptions{
		Version:             versionKey,
		AgentVariant:        agentVariant,
		ReusePreviousConfig: reusePreviousConfig,
	}, func(evt map[string]interface{}) error {
		typ, _ := evt["type"].(string)
		switch typ {
		case "start":
			if t, ok := evt["total"].(float64); ok {
				doneTotal = int(t)
			}
			if v, _ := evt["version"].(string); strings.TrimSpace(v) != "" {
				versionKey = strings.TrimSpace(v)
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
				ver := ""
				if v, _ := evt["version"].(string); strings.TrimSpace(v) != "" {
					ver = " (" + strings.TrimSpace(v) + ")"
				}
				suffix := ""
				if connectIP, _ := evt["connect_ip"].(string); strings.TrimSpace(connectIP) != "" {
					suffix = " via " + strings.TrimSpace(connectIP)
				}
				fmt.Println(prefix + "update apply requested" + ver + suffix)
			case "skipped":
				skipped++
				msg, _ := evt["message"].(string)
				if strings.TrimSpace(msg) == "" {
					msg = "not applicable"
				}
				fmt.Println(prefix + "skipped — " + msg)
			case "fail":
				failed++
				msg, _ := evt["message"].(string)
				if strings.TrimSpace(msg) == "" {
					msg = "unknown error"
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
			if sk, ok := evt["skipped"].(float64); ok {
				skipped = int(sk)
			}
			if t, ok := evt["total"].(float64); ok {
				doneTotal = int(t)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: apply-update-all: %v\n", appmeta.BinaryName, err)
		return 1
	}

	if doneTotal == 0 {
		fmt.Println("No hosts were processed.")
		return 1
	}
	if skipped > 0 || failed > 0 {
		fmt.Printf("Done: succeeded=%d failed=%d skipped=%d (total %d).\n", succeeded, failed, skipped, doneTotal)
	} else {
		fmt.Printf("Done: all %d host(s) received update apply.\n", succeeded)
	}
	if failed > 0 || succeeded == 0 {
		return 1
	}
	return 0
}
