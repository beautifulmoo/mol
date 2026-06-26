// Package configpushclicli implements `agent --push-config-all-remotes` (UDP discovery + POST push-local-all).
package configpushclicli

import (
	"flag"
	"fmt"
	"os"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/bulkcli"
	"contrabass-agent/maintenance/clirest"
)

// Run parses flags and pushes local current config to all remotes found by discovery.
//
//	<bin> agent --push-config-all-remotes [-apiprefix <path>] [-maintenance-port=N]
func Run(args []string) int {
	fs := flag.NewFlagSet("push-config-all-remotes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	apiPrefixFlag, maintenancePortFlag := bulkcli.RegisterMaintenanceFlags(fs)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --push-config-all-remotes [-apiprefix=<path>] [-maintenance-port=N]\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  POST {APIPrefix}/current-config/push-local-all on the local maintenance HTTP listener\n")
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
		Operation:       bulkcli.OpPushConfig,
		CmdName:         appmeta.BinaryName,
		EmptyHostsMsg:   "No remote hosts found; nothing to push.",
		FoundHostsFmt:   "Found %d remote host(s) for config push.\n",
		StartFmt:        "Pushing local current config to %d host(s)...\n",
		StreamErrLabel:  "push-local-all",
	}, clirest.PushLocalConfigAll)
}
