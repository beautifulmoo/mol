package server

import (
	"os"
	"path/filepath"

	"contrabass-agent/maintenance/versionsapi"
)

// stagingDir returns deploy_base/staging/<version>. Staging is never the running path, so no "text file busy".
func (s *Server) stagingDir(base, version string) string {
	return filepath.Join(base, "staging", version)
}

// clearStaging removes the entire deploy_base/staging/ directory so that upload replaces all staging content with the new version only.
func (s *Server) clearStaging(base string) {
	stagingParent := filepath.Join(base, "staging")
	_ = os.RemoveAll(stagingParent)
}

// versionsDir returns base/versions/<version> (the running path).
func (s *Server) versionsDir(base, version string) string {
	return filepath.Join(base, "versions", version)
}

// versionsBase returns the base path for versions/ (install_prefix or deploy_base). Used for list/remove and installer.
func (s *Server) versionsBase() string {
	return versionsapi.VersionsBaseFromParts(s.installPrefix, s.deployBase)
}

// resolveVersionDir returns the directory that contains the agent binary + config for this version: staging first (under base/deploy), then versions/ under versionsBase() (same tree as GET /versions/list).
func (s *Server) resolveVersionDir(base, version string) (string, bool) {
	stg := s.stagingDir(base, version)
	if versionsapi.DirHasStagedAgents(stg) {
		return stg, true // from staging
	}
	ver := filepath.Join(s.versionsBase(), "versions", version)
	if versionsapi.DirHasStagedAgents(ver) {
		return ver, false // from versions
	}
	return "", false
}
