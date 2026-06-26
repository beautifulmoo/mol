package bulkcli

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discoverycli"
)

// Runner matches clirest bulk NDJSON entry points (except ApplyUpdateAll).
type Runner func(client *http.Client, apiPrefix string, maintenancePort int, hosts []clirest.BulkPushHost, onEvent func(map[string]interface{}) error) error

// Config is shared orchestration for cached-host bulk commands (REPL).
type Config struct {
	APIPrefix       string
	MaintenancePort int
	Operation       Operation
	Hosts           []clirest.BulkPushHost
	StartFmt        string
}

// DiscoveryConfig adds UDP discovery prelude for standalone bulk CLIs.
type DiscoveryConfig struct {
	APIPrefix       string
	MaintenancePort int
	Operation       Operation
	CmdName         string
	EmptyHostsMsg   string
	FoundHostsFmt   string
	StartFmt        string
	StreamErrLabel  string
}

// Run executes maintenance check, streams NDJSON, and returns a process exit code.
func Run(cfg Config, run Runner) int {
	if err := RunError(cfg, run); err != nil {
		return 1
	}
	return 0
}

// RunError is like Run but returns an error for REPL callers.
func RunError(cfg Config, run Runner) error {
	if len(cfg.Hosts) == 0 {
		return fmt.Errorf("no hosts")
	}
	check := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(check, cfg.APIPrefix, cfg.MaintenancePort); err != nil {
		return err
	}
	if cfg.StartFmt != "" {
		fmt.Printf(cfg.StartFmt, len(cfg.Hosts))
	}
	var c Counters
	streamClient := clirest.DefaultHTTPClient(0)
	if err := run(streamClient, cfg.APIPrefix, cfg.MaintenancePort, cfg.Hosts, ProgressHandler(cfg.Operation, &c)); err != nil {
		return err
	}
	return FinishSummary(cfg.Operation, c, nil)
}

func formatCmdErr(cmdName, label string, err error) string {
	if cmdName == "" {
		if label == "" {
			return err.Error()
		}
		return label + ": " + err.Error()
	}
	if label == "" {
		return cmdName + ": " + err.Error()
	}
	return cmdName + ": " + label + ": " + err.Error()
}

// RunDiscovery resolves hosts via default UDP discovery, then runs the bulk stream.
func RunDiscovery(cfg DiscoveryConfig, run Runner) int {
	check := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(check, cfg.APIPrefix, cfg.MaintenancePort); err != nil {
		fmt.Fprintln(os.Stderr, formatCmdErr(cfg.CmdName, "", err))
		return 1
	}
	list, err := discoverycli.RunDefaultDiscoveryToStdout()
	if err != nil {
		fmt.Fprintln(os.Stderr, formatCmdErr(cfg.CmdName, "discovery", err))
		return 1
	}
	hosts := discoverycli.BulkPushHostsFromDiscovery(list)
	if len(hosts) == 0 {
		if cfg.EmptyHostsMsg != "" {
			fmt.Println(cfg.EmptyHostsMsg)
		}
		return 1
	}
	if cfg.FoundHostsFmt != "" {
		fmt.Printf(cfg.FoundHostsFmt, len(hosts))
	}
	err = RunError(Config{
		APIPrefix:       cfg.APIPrefix,
		MaintenancePort: cfg.MaintenancePort,
		Operation:       cfg.Operation,
		Hosts:           hosts,
		StartFmt:        cfg.StartFmt,
	}, run)
	if err != nil {
		label := cfg.StreamErrLabel
		if label == "" {
			label = "bulk"
		}
		fmt.Fprintln(os.Stderr, formatCmdErr(cfg.CmdName, label, err))
		return 1
	}
	return 0
}
