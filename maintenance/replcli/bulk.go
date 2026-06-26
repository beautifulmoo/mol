package replcli

import (
	"strings"

	"contrabass-agent/maintenance/bulkcli"
	"contrabass-agent/maintenance/clirest"
)

func runPushConfigAll(s *Session) error {
	return runCachedBulk(s, "Pushing local current config to %d cached host(s)...\n", bulkcli.OpPushConfig, clirest.PushLocalConfigAll)
}

func runRestartAll(s *Session) error {
	return runCachedBulk(s, "Restarting %d cached host(s)...\n", bulkcli.OpRestart, clirest.RestartAllRemotes)
}

func runRollbackAll(s *Session) error {
	return runCachedBulk(s, "Rolling back %d cached host(s)...\n", bulkcli.OpRollback, clirest.RollbackAllRemotes)
}

func runCachedBulk(s *Session, startFmt string, op bulkcli.Operation, run bulkcli.Runner) error {
	hosts, err := s.requireCachedHosts()
	if err != nil {
		return err
	}
	return bulkcli.RunError(bulkcli.Config{
		APIPrefix:       s.APIPrefix,
		MaintenancePort: s.MaintenancePort,
		Operation:       op,
		Hosts:           hosts,
		StartFmt:        startFmt,
	}, run)
}

func runApplyUpdateAll(s *Session, bundlePath, agentVariantOverride string, useBundleOverride bool) error {
	hosts, err := s.requireCachedHosts()
	if err != nil {
		return err
	}
	agentVariantFlag := strings.TrimSpace(agentVariantOverride)
	if agentVariantFlag == "" {
		agentVariantFlag = strings.TrimSpace(s.AgentVariant)
	}
	return bulkcli.RunApplyCached(bulkcli.ApplyCachedConfig{
		APIPrefix:       s.APIPrefix,
		MaintenancePort: s.MaintenancePort,
		BundlePath:      bundlePath,
		AgentVariant:    agentVariantFlag,
		CLIBuildVariant: s.CLIBuildVariant,
		UseBundleConfig: useBundleOverride,
		Hosts:           hosts,
	})
}
