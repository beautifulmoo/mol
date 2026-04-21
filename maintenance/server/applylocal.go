package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/versionsapi"
)

// ApplyUpdateSelfFromBundleExtract stages the validated bundle under DeployBase and runs local apply
// (same effect as POST /upload then POST /apply-update with ip:self). Caller must have already run
// PrepareAgentBundleFromReader with the same raw tar.gz bytes; agentSrc is the extracted binary path inside workDir.
// raw is the original bundle bytes (for StagedBundleFileName). Caller typically needs root/sudo for deploy tree and systemd-run.
func ApplyUpdateSelfFromBundleExtract(cfg *config.Config, raw []byte, versionKey string, configData []byte, agentSrc string) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	base := versionsapi.DeployRootFromConfig(cfg)
	_ = os.RemoveAll(filepath.Join(base, "staging"))

	finalDir := filepath.Join(base, "staging", versionKey)
	if err := os.MkdirAll(filepath.Join(base, "staging"), 0755); err != nil {
		return fmt.Errorf("staging dir: %w", err)
	}
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return fmt.Errorf("staging version dir: %w", err)
	}

	binDst := filepath.Join(finalDir, appmeta.BinaryName)
	srcf, err := os.Open(agentSrc)
	if err != nil {
		return fmt.Errorf("open extracted binary: %w", err)
	}
	dstf, err := os.OpenFile(binDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		_ = srcf.Close()
		return fmt.Errorf("write staged binary: %w", err)
	}
	_, err = io.Copy(dstf, srcf)
	_ = srcf.Close()
	_ = dstf.Close()
	if err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("copy binary to staging: %w", err)
	}
	if err := os.WriteFile(filepath.Join(finalDir, "config.yaml"), configData, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("write staged config: %w", err)
	}
	if err := validateAgentBinary(binDst); err != nil {
		_ = os.RemoveAll(finalDir)
		return err
	}
	if err := os.WriteFile(filepath.Join(finalDir, StagedBundleFileName), raw, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("write staged bundle copy: %w", err)
	}
	return versionsapi.RunSwitchCurrentWithRoots(base, cfg.InstallPrefix, cfg.DeployBase, versionKey)
}
