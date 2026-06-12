package server

import (
	"fmt"
	"os"
	"path/filepath"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
)

// ConfigBackupBasename is the filename used when backing up current config before overwrite.
const ConfigBackupBasename = appmeta.ConfigFileName + ".backup"

func configBackupPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), ConfigBackupBasename)
}

// backupCurrentConfigFile copies the existing config at configPath to agent.local.yml.backup
// in the same directory. Missing config is not an error.
func backupCurrentConfigFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(configBackupPath(configPath), data, 0644)
}

func validateCurrentConfigContent(content string) error {
	trimmed := trimConfigContent(content)
	if trimmed == "" {
		return nil
	}
	if _, err := agentcfg.LoadFromBytes([]byte(trimmed)); err != nil {
		return err
	}
	return nil
}

func trimConfigContent(content string) string {
	// Preserve intentional trailing newline in saved file; only reject all-whitespace bodies.
	if content == "" {
		return ""
	}
	for _, r := range content {
		if r != ' ' && r != '\t' && r != '\r' && r != '\n' {
			return content
		}
	}
	return ""
}

// saveCurrentConfigContent writes content to configPath. When backupBeforeWrite is true,
// the previous file (if any) is copied to agent.local.yml.backup first.
func saveCurrentConfigContent(configPath, content string, backupBeforeWrite bool) error {
	if backupBeforeWrite {
		if err := backupCurrentConfigFile(configPath); err != nil {
			return fmt.Errorf("%s backup failed: %w", ConfigBackupBasename, err)
		}
	}
	if err := validateCurrentConfigContent(content); err != nil {
		return err
	}
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("%s save failed: %w", appmeta.ConfigFileName, err)
	}
	return nil
}
