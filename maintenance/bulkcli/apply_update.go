package bulkcli

import (
	"fmt"
	"os"
	"time"

	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discoverycli"
)

// ApplyDiscoveryConfig runs upload + discovery + apply-update-all for standalone CLIs.
type ApplyDiscoveryConfig struct {
	APIPrefix        string
	MaintenancePort  int
	BundlePath       string
	AgentVariantFlag string
	CLIBuildVariant  string
	UseBundleConfig  bool
	CmdName          string
	EmptyHostsMsg    string
	FoundHostsFmt    string
	StartFmt         string
	StreamErrLabel   string
}

// ApplyCachedConfig runs upload + apply-update-all for REPL cached hosts.
type ApplyCachedConfig struct {
	APIPrefix       string
	MaintenancePort int
	BundlePath      string
	AgentVariant    string
	CLIBuildVariant string
	UseBundleConfig bool
	Hosts           []clirest.BulkPushHost
}

// RunApplyDiscovery uploads, discovers remotes, and streams apply-update-all.
func RunApplyDiscovery(cfg ApplyDiscoveryConfig) int {
	check := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(check, cfg.APIPrefix, cfg.MaintenancePort); err != nil {
		fmt.Fprintln(os.Stderr, formatCmdErr(cfg.CmdName, "", err))
		return 1
	}
	versionKey, variant, reusePrevious, err := stageBundleForApply(cfg.APIPrefix, cfg.MaintenancePort, cfg.BundlePath, cfg.AgentVariantFlag, cfg.CLIBuildVariant, cfg.UseBundleConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, formatCmdErr(cfg.CmdName, "upload", err))
		return 1
	}
	fmt.Printf("Staged version %s (variant %s, reuse_previous_config=%v).\n", versionKey, variant, reusePrevious)

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
	startFmt := cfg.StartFmt
	if startFmt == "" {
		startFmt = "Applying version %s to %d remote host(s)...\n"
	}
	fmt.Printf(startFmt, versionKey, len(hosts))

	if err := runApplyStream(cfg.APIPrefix, cfg.MaintenancePort, hosts, versionKey, variant, reusePrevious); err != nil {
		label := cfg.StreamErrLabel
		if label == "" {
			label = "apply-update-all"
		}
		fmt.Fprintln(os.Stderr, formatCmdErr(cfg.CmdName, label, err))
		return 1
	}
	return 0
}

// RunApplyCached uploads and applies to pre-resolved cached hosts (REPL).
func RunApplyCached(cfg ApplyCachedConfig) error {
	if len(cfg.Hosts) == 0 {
		return fmt.Errorf("no cached hosts")
	}
	check := clirest.DefaultHTTPClient(5 * time.Second)
	if err := clirest.EnsureMaintenanceRunning(check, cfg.APIPrefix, cfg.MaintenancePort); err != nil {
		return err
	}
	agentVariant := cfg.AgentVariant
	if agentVariant == "" {
		agentVariant = cfg.CLIBuildVariant
	}
	if agentVariant == "" {
		agentVariant = "compute"
	}
	versionKey, variant, reusePrevious, err := stageBundleForApply(cfg.APIPrefix, cfg.MaintenancePort, cfg.BundlePath, agentVariant, cfg.CLIBuildVariant, cfg.UseBundleConfig)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	fmt.Printf("Staged version %s (variant %s, reuse_previous_config=%v).\n", versionKey, variant, reusePrevious)
	fmt.Printf("Applying version %s to %d cached host(s)...\n", versionKey, len(cfg.Hosts))
	return runApplyStream(cfg.APIPrefix, cfg.MaintenancePort, cfg.Hosts, versionKey, variant, reusePrevious)
}

func stageBundleForApply(apiPrefix string, maintenancePort int, bundlePath, agentVariantFlag, cliBuildVariant string, useBundleConfig bool) (versionKey, variant string, reusePrevious bool, err error) {
	installedBV := cliBuildVariant
	if installedBV == "" {
		installedBV = "compute"
	}
	variant, err = clirest.ResolveAgentVariantForTarget(agentVariantFlag, installedBV)
	if err != nil {
		return "", "", false, err
	}
	reusePrevious = !useBundleConfig
	fmt.Printf("Uploading bundle %s to local staging...\n", bundlePath)
	uploadClient := clirest.DefaultHTTPClient(300 * time.Second)
	versionKey, err = clirest.UploadBundleMaintenance(uploadClient, apiPrefix, maintenancePort, bundlePath)
	if err != nil {
		return "", "", false, err
	}
	return versionKey, variant, reusePrevious, nil
}

func runApplyStream(apiPrefix string, maintenancePort int, hosts []clirest.BulkPushHost, versionKey, variant string, reusePrevious bool) error {
	var c Counters
	streamClient := clirest.DefaultHTTPClient(0)
	opts := clirest.ApplyUpdateAllOptions{
		Version:             versionKey,
		AgentVariant:        variant,
		ReusePreviousConfig: reusePrevious,
	}
	err := clirest.ApplyUpdateAll(streamClient, apiPrefix, maintenancePort, hosts, opts, func(evt map[string]interface{}) error {
		if typ, _ := evt["type"].(string); typ == "start" {
			if v, ok := evt["version"].(string); ok && v != "" {
				versionKey = v
			}
		}
		return printBulkProgress(evt, OpApplyUpdate, &c)
	})
	if err != nil {
		return err
	}
	return FinishSummary(OpApplyUpdate, c, nil)
}
