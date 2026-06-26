package server

import (
	"fmt"
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/versionsapi"
)

// elfMagic is the first 4 bytes of an ELF executable.
var elfMagic = []byte{0x7f, 'E', 'L', 'F'}

func isELFExecutable(header []byte) bool {
	return len(header) >= 4 && header[0] == elfMagic[0] && header[1] == elfMagic[1] && header[2] == elfMagic[2] && header[3] == elfMagic[3]
}

// VersionKeyFromAgentBinary runs binPath --version, or on failure agent --version, and returns the version key after "<BinaryName> " (same as POST /upload validation). Exported for CLI.
func VersionKeyFromAgentBinary(binPath string) (string, error) {
	return versionKeyFromAgentBinary(binPath)
}

// BuildVariantFromAgentBinary runs the same --version invocations and returns "(control)" or "(compute)" from the line, or "" if absent.
func BuildVariantFromAgentBinary(binPath string) (string, error) {
	line, err := versionsapi.AgentVersionLine(binPath)
	if err != nil {
		return "", err
	}
	return appmeta.ParseBuildVariantFromVersionLine(line), nil
}

// versionKeyFromAgentBinary tries root --version first (legacy / transitional updates), then agent --version.
func versionKeyFromAgentBinary(binPath string) (string, error) {
	line, err := versionsapi.AgentVersionLine(binPath)
	if err != nil {
		return "", fmt.Errorf("not a valid executable (--version: %v)", err)
	}
	want := appmeta.BinaryName + " "
	if !strings.HasPrefix(line, want) {
		return "", fmt.Errorf("prefix want %q, got %q", want, line)
	}
	key := strings.TrimSpace(strings.TrimPrefix(line, want))
	if idx := strings.Index(key, " ("); idx >= 0 {
		key = strings.TrimSpace(key[:idx])
	}
	if key == "" {
		return "", fmt.Errorf("empty version key")
	}
	if err := agentcfg.ValidateVersionKeyPath(key); err != nil {
		return "", err
	}
	return key, nil
}

// validateAgentBinary runs the same version checks as bundle upload (root --version, then agent --version).
func validateAgentBinary(binPath string) error {
	_, err := versionKeyFromAgentBinary(binPath)
	return err
}
