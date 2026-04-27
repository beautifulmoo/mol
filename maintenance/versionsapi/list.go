// Package versionsapi holds shared logic for GET /versions/list (local filesystem scan).
package versionsapi

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"contrabass-agent/maintenance/config"
	"contrabass-agent/maintenance/appmeta"
)

// VersionEntry is one row in GET /api/v1/versions/list `data.versions`.
type VersionEntry struct {
	Version    string `json:"version"`
	IsCurrent  bool   `json:"is_current"`
	IsPrevious bool   `json:"is_previous"`
}

// VersionsBaseFromParts mirrors server.Server.versionsBase after config normalization: InstallPrefix, else DeployBase, else default.
func VersionsBaseFromParts(installPrefix, deployBase string) string {
	inst := strings.TrimSpace(strings.TrimSuffix(installPrefix, "/"))
	dep := strings.TrimSpace(strings.TrimSuffix(deployBase, "/"))
	base := inst
	if base == "" {
		base = dep
	}
	if base == "" {
		return "/var/lib/contrabass/mole"
	}
	return base
}

// VersionsBaseFromConfig uses YAML fields (same defaults as maintenance/config.Default().DeployBase when unset).
func VersionsBaseFromConfig(cfg *config.Config) string {
	if cfg == nil {
		return "/var/lib/contrabass/mole"
	}
	return VersionsBaseFromParts(cfg.InstallPrefix, cfg.DeployBase)
}

// ListInstalledVersions returns installed version directories under base/versions/ that contain the agent binary,
// with current/previous flags and the same sort order as the HTTP handler (GET .../versions/list, local).
func ListInstalledVersions(base string) ([]VersionEntry, error) {
	base = strings.TrimSpace(strings.TrimSuffix(base, "/"))
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	versionsParent := filepath.Join(base, "versions")
	entries, err := os.ReadDir(versionsParent)
	if err != nil {
		if os.IsNotExist(err) {
			return []VersionEntry{}, nil
		}
		return nil, err
	}
	currentVer := ResolveSymlinkVersion(base, "current")
	previousVer := ResolveSymlinkVersion(base, "previous")
	var list []VersionEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ver := e.Name()
		if ver == "." || ver == ".." {
			continue
		}
		if !dirHasAgentBinary(filepath.Join(versionsParent, ver)) {
			continue
		}
		list = append(list, VersionEntry{
			Version:    ver,
			IsCurrent:  ver == currentVer,
			IsPrevious: ver == previousVer,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return versionsListEntryBefore(list[i], list[j])
	})
	return list, nil
}

// ResolveSymlinkVersion returns the top-level version directory name that base/name (e.g. current, previous) points to, or "".
func ResolveSymlinkVersion(base, name string) string {
	linkPath := filepath.Join(base, name)
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return ""
	}
	versionsDir := filepath.Join(base, "versions")
	rel, err := filepath.Rel(versionsDir, resolved)
	if err != nil {
		return ""
	}
	if rel == ".." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

func dirHasAgentBinary(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, appmeta.BinaryName))
	return err == nil
}

// versionsListEntryBefore defines display order: current → previous → others by semver descending (newest first).
func versionsListEntryBefore(a, b VersionEntry) bool {
	rank := func(e VersionEntry) int {
		if e.IsCurrent {
			return 2
		}
		if e.IsPrevious {
			return 1
		}
		return 0
	}
	ra, rb := rank(a), rank(b)
	if ra != rb {
		return ra > rb
	}
	return config.CompareVersionKeys(a.Version, b.Version) > 0
}
