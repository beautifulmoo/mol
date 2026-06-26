package versionsapi

import (
	"os"
	"path/filepath"

	"contrabass-agent/maintenance/appmeta"
)

// StagingHasDualAgents reports control+compute binaries present (manifest v2 layout) without requiring BinaryName yet.
func StagingHasDualAgents(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range []string{appmeta.BundleAgentControlName, appmeta.BundleAgentComputeName} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.IsDir() {
			return false
		}
	}
	return true
}

// DirHasStagedAgents is true when a staging/version dir can be applied: canonical binary and/or dual bundle agents.
func DirHasStagedAgents(dir string) bool {
	if DirHasAgentBinary(dir) {
		return true
	}
	return StagingHasDualAgents(dir)
}
