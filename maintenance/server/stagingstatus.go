package server

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/versionsapi"
)

func (s *Server) deployBaseOrDefault() string {
	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	return base
}

func (s *Server) localStagingVersions() []string {
	base := s.deployBaseOrDefault()
	stagingParent := filepath.Join(base, "staging")
	stagingVersions := []string{}
	if entries, err := os.ReadDir(stagingParent); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			v := e.Name()
			if versionsapi.DirHasStagedAgents(filepath.Join(stagingParent, v)) {
				stagingVersions = append(stagingVersions, v)
			}
		}
	}
	sort.Slice(stagingVersions, func(i, j int) bool {
		return agentcfg.CompareVersionKeys(stagingVersions[i], stagingVersions[j]) > 0
	})
	return stagingVersions
}

func (s *Server) stagingApplyDecisionForCompareKey(compareKey string) (canApply bool, applyVersion string) {
	compareKey = strings.TrimSpace(compareKey)
	for _, v := range s.localStagingVersions() {
		if agentcfg.StagingUpdateAvailable(v, compareKey, s.allowSameVersionUpdate) {
			return true, v
		}
	}
	return false, ""
}
