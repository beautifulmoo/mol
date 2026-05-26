package server

import (
	"fmt"
	"path/filepath"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/versionsapi"
)

// MaterializeCanonicalAgent copies the selected bundle agent (control or compute) to BinaryName in versionDir.
// Legacy trees with only BinaryName and no dual agents are left unchanged.
func MaterializeCanonicalAgent(versionDir, agentVariant string) error {
	variant, err := appmeta.ParseAgentVariant(agentVariant)
	if err != nil {
		return err
	}
	if versionDir == "" {
		return fmt.Errorf("version directory is empty")
	}
	if !versionsapi.StagingHasDualAgents(versionDir) {
		if !dirHasAgentBinary(versionDir) {
			return fmt.Errorf("버전 디렉터리에 %s 또는 %s/%s 가 없습니다",
				appmeta.BinaryName, appmeta.BundleAgentControlName, appmeta.BundleAgentComputeName)
		}
		return nil
	}
	srcName := appmeta.BundleAgentBasenameForVariant(variant)
	src := filepath.Join(versionDir, srcName)
	dst := filepath.Join(versionDir, appmeta.BinaryName)
	if err := copyFile(src, dst, 0755); err != nil {
		return fmt.Errorf("copy %s → %s: %w", srcName, appmeta.BinaryName, err)
	}
	return validateAgentBinary(dst)
}
