package server

import (
	"fmt"
	"os"
	"path/filepath"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/versionsapi"
)

// ApplyUpdateSelfFromBundleExtract stages the validated bundle under DeployBase and runs local apply
// (same effect as POST /upload then POST /apply-update with ip:self). Caller must have already run
// PrepareAgentBundleFromReader with the same raw tar.gz bytes.
// raw is the original bundle bytes (for StagedBundleFileName). Caller typically needs root/sudo for deploy tree and systemd-run.
func ApplyUpdateSelfFromBundleExtract(cfg *agentcfg.Config, raw []byte, pb *PreparedBundle, agentVariant string) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if pb == nil {
		return fmt.Errorf("prepared bundle is nil")
	}
	base := versionsapi.DeployRootFromConfig(cfg)
	_ = os.RemoveAll(filepath.Join(base, "staging"))

	finalDir := filepath.Join(base, "staging", pb.VersionKey)
	if err := os.MkdirAll(filepath.Join(base, "staging"), 0755); err != nil {
		return fmt.Errorf("staging dir: %w", err)
	}
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return fmt.Errorf("staging version dir: %w", err)
	}

	if err := StagePreparedBundle(finalDir, pb); err != nil {
		_ = os.RemoveAll(finalDir)
		return err
	}
	if err := os.WriteFile(filepath.Join(finalDir, StagedBundleFileName), raw, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("write staged bundle copy: %w", err)
	}
	return versionsapi.RunSwitchCurrentWithRootsVariant(base, cfg.InstallPrefix, cfg.DeployBase, pb.VersionKey, agentVariant, "")
}
