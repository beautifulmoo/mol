package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"contrabass-agent/maintenance/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/versionsapi"
)

func copyFileLocal(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}

// ApplyUpdateSelfFromBundleExtract stages the validated bundle under DeployBase and runs local apply
// (same effect as POST /upload then POST /apply-update with ip:self). Caller must have already run
// PrepareAgentBundleFromReader with the same raw tar.gz bytes; agentSrc is the extracted binary path inside workDir.
// raw is the original bundle bytes (for StagedBundleFileName). Caller typically needs root/sudo for deploy tree and systemd-run.
func ApplyUpdateSelfFromBundleExtract(cfg *config.Config, raw []byte, versionKey string, configData []byte, agentFileName string, configFileName string, agentSrc string) error {
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

	if strings.TrimSpace(agentFileName) == "" {
		agentFileName = appmeta.BinaryName
	}
	manifestBinDst := filepath.Join(finalDir, agentFileName)
	srcf, err := os.Open(agentSrc)
	if err != nil {
		return fmt.Errorf("open extracted binary: %w", err)
	}
	dstf, err := os.OpenFile(manifestBinDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
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
	if agentFileName != appmeta.BinaryName {
		if err := copyFileLocal(manifestBinDst, filepath.Join(finalDir, appmeta.BinaryName), 0755); err != nil {
			_ = os.RemoveAll(finalDir)
			return fmt.Errorf("copy staged binary: %w", err)
		}
	}
	if strings.TrimSpace(configFileName) == "" {
		configFileName = appmeta.ConfigFileName
	}
	if err := os.WriteFile(filepath.Join(finalDir, configFileName), configData, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("write staged config: %w", err)
	}
	if err := validateAgentBinary(filepath.Join(finalDir, appmeta.BinaryName)); err != nil {
		_ = os.RemoveAll(finalDir)
		return err
	}
	if err := os.WriteFile(filepath.Join(finalDir, StagedBundleFileName), raw, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		return fmt.Errorf("write staged bundle copy: %w", err)
	}
	return versionsapi.RunSwitchCurrentWithRoots(base, cfg.InstallPrefix, cfg.DeployBase, versionKey)
}
