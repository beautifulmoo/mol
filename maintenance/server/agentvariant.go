package server

import (
	"os"
	"path/filepath"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/versionsapi"
)

// MaterializeCanonicalAgent copies the selected bundle agent (control or compute) to BinaryName in versionDir.
// Legacy trees with only BinaryName and no dual agents are left unchanged.
func MaterializeCanonicalAgent(versionDir, agentVariant string) error {
	if err := versionsapi.MaterializeCanonicalAgent(versionDir, agentVariant); err != nil {
		return err
	}
	dst := filepath.Join(versionDir, appmeta.BinaryName)
	if _, err := os.Stat(dst); err != nil {
		return err
	}
	return validateAgentBinary(dst)
}
