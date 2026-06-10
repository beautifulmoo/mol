package versionsapi

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"contrabass-agent/maintenance/appmeta"
)

// CopyPreviousConfigToVersion copies the config from the pre-update current version tree into
// targetVersionDir. It resolves deployRoot/current first, then versionsBase/current when InstallPrefix
// differs from DeployBase. Missing current or config is not an error.
func CopyPreviousConfigToVersion(deployRoot, installPrefix, deployBaseRaw, targetVersionDir string) error {
	deployRoot = strings.TrimSuffix(strings.TrimSpace(deployRoot), "/")
	targetVersionDir = strings.TrimSpace(targetVersionDir)
	if deployRoot == "" || targetVersionDir == "" {
		return fmt.Errorf("deploy root and target version directory are required")
	}
	currentResolved := resolvePreUpdateCurrentDir(deployRoot, installPrefix, deployBaseRaw)
	if currentResolved == "" {
		log.Printf("reuse previous config: skip (no current symlink under deploy root or versions base)")
		return nil
	}
	configName := ConfigBasenameInVersionDir(targetVersionDir)
	src := filepath.Join(currentResolved, configName)
	if _, err := os.Stat(src); err != nil {
		log.Printf("reuse previous config: skip (%s not in current %s): %v", configName, currentResolved, err)
		return nil
	}
	dst := filepath.Join(targetVersionDir, configName)
	if err := copyFileRobust(src, dst, 0644); err != nil {
		return fmt.Errorf("copy current %s to %s: %w", configName, targetVersionDir, err)
	}
	log.Printf("reuse previous config: copied %s from current %s to %s", configName, currentResolved, targetVersionDir)
	return nil
}

// OverwriteVersionDirConfig writes content to the config file basename found in versionDir.
func OverwriteVersionDirConfig(versionDir, content string) error {
	versionDir = strings.TrimSpace(versionDir)
	if versionDir == "" {
		return fmt.Errorf("version directory is empty")
	}
	name := ConfigBasenameInVersionDir(versionDir)
	dst := filepath.Join(versionDir, name)
	if err := os.WriteFile(dst, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func resolvePreUpdateCurrentDir(deployRoot, installPrefix, deployBaseRaw string) string {
	seen := map[string]struct{}{}
	tryBase := func(base string) string {
		base = strings.TrimSuffix(strings.TrimSpace(base), "/")
		if base == "" {
			return ""
		}
		if _, ok := seen[base]; ok {
			return ""
		}
		seen[base] = struct{}{}
		linkPath := filepath.Join(base, "current")
		resolved, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			return ""
		}
		if st, err := os.Stat(resolved); err == nil && st.IsDir() {
			return resolved
		}
		return ""
	}
	if dir := tryBase(deployRoot); dir != "" {
		return dir
	}
	inst := strings.TrimSpace(installPrefix)
	dep := strings.TrimSpace(strings.TrimSuffix(deployBaseRaw, "/"))
	if inst != "" || dep != "" {
		return tryBase(VersionsBaseFromParts(installPrefix, deployBaseRaw))
	}
	return ""
}

// ConfigBasenameInVersionDir returns the config filename to use in a staged/installed version tree.
func ConfigBasenameInVersionDir(targetVersionDir string) string {
	entries, err := os.ReadDir(targetVersionDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == appmeta.ConfigFileName {
				return name
			}
			if strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") {
				return name
			}
		}
	}
	return appmeta.ConfigFileName
}
