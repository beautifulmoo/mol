// Package rollbackallclicli implements `agent --rollback-all-remotes` (UDP discovery + POST rollback-all).
package rollbackallclicli

import (
	"flag"
	"fmt"
	"os"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/bulkcli"
	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discoverycli"
)

// Run parses flags and rolls back all eligible remotes found by discovery.
//
//	<bin> agent --rollback-all-remotes [-apiprefix <path>] [-maintenance-port=N]
func Run(args []string) int {
	fs := flag.NewFlagSet("rollback-all-remotes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apiPrefixFlag := fs.String("apiprefix", "", fmt.Sprintf("maintenance API path prefix (default %s)", clirest.DefaultAPIPrefix))
	maintenancePortFlag := fs.Int("maintenance-port", clirest.DefaultMaintenancePort, "local maintenance HTTP port (orchestrator)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --rollback-all-remotes [-apiprefix=<path>] [-maintenance-port=N]\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Runs UDP discovery (default ports/timeouts), then POST {APIPrefix}/versions/rollback-all\n")
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

	return bulkcli.RunDiscovery(bulkcli.DiscoveryConfig{
		APIPrefix:       *apiPrefixFlag,
		MaintenancePort: *maintenancePortFlag,
		Operation:       bulkcli.OpRollback,
		CmdName:         appmeta.BinaryName,
		EmptyHostsMsg:   "No remote hosts found; nothing to roll back.",
		FoundHostsFmt:   "Found %d remote host(s) for rollback.\n",
		StartFmt:        "Rolling back %d remote host(s)...\n",
		StreamErrLabel:  "rollback-all",
	}, clirest.RollbackAllRemotes)
}
