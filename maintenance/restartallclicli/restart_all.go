// Package restartallclicli implements `agent --restart-all-remotes` (UDP discovery + POST restart-all).
package restartallclicli

import (
	"flag"
	"fmt"
	"os"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/bulkcli"
	"contrabass-agent/maintenance/clirest"
)

// Run parses flags and restarts all remotes found by discovery.
//
//	<bin> agent --restart-all-remotes [-apiprefix <path>] [-maintenance-port=N]
func Run(args []string) int {
	fs := flag.NewFlagSet("restart-all-remotes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apiPrefixFlag, maintenancePortFlag := bulkcli.RegisterMaintenanceFlags(fs)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --restart-all-remotes [-apiprefix=<path>] [-maintenance-port=N]\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  POST {APIPrefix}/service-control/restart-all on the local maintenance HTTP listener\n")
		fmt.Fprintf(os.Stderr, "  with hosts discovered via built-in UDP discovery.\n")
		fmt.Fprintf(os.Stderr, "  The orchestrator maintenance service must be running (%s -cfg <file>).\n\n", appmeta.BinaryName)
		bulkcli.WriteDiscoveryBuiltInHelp(os.Stderr)
		bulkcli.WriteMaintenanceFlagHelp(os.Stderr)
	}
	if bulkcli.CheckHelpArgs(args, fs.Usage) {
		return 0
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected arguments (this command takes no positional args)\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}
	if err := bulkcli.ValidateMaintenancePort(*maintenancePortFlag); err != nil {
		bulkcli.FormatMaintenancePortError(err)
		return 1
	}

	return bulkcli.RunDiscovery(bulkcli.DiscoveryConfig{
		APIPrefix:       *apiPrefixFlag,
		MaintenancePort: *maintenancePortFlag,
		Operation:       bulkcli.OpRestart,
		CmdName:         appmeta.BinaryName,
		EmptyHostsMsg:   "No remote hosts found; nothing to restart.",
		FoundHostsFmt:   "Found %d remote host(s) for restart.\n",
		StartFmt:        "Restarting %d remote host(s)...\n",
		StreamErrLabel:  "restart-all",
	}, clirest.RestartAllRemotes)
}
