package replcli

import (
	"fmt"
	"strings"

	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/discoverycli"
)

// Session holds REPL defaults and the last discovery snapshot.
type Session struct {
	CLIBuildVariant string
	APIPrefix       string
	MaintenancePort int
	AgentVariant    string
	UseBundleConfig bool

	LastDiscovery []discovery.DiscoveryResponse
	CachedHosts   []clirest.BulkPushHost
}

func newSession(cliBuildVariant string) *Session {
	return &Session{
		MaintenancePort: clirest.DefaultMaintenancePort,
		CLIBuildVariant: strings.TrimSpace(cliBuildVariant),
	}
}

func (s *Session) effectiveAPIPrefix() string {
	return clirest.NormalizeAPIPrefix(s.APIPrefix)
}

func (s *Session) ginArgs(extra ...string) []string {
	out := make([]string, 0, 2+len(extra))
	if p := strings.TrimSpace(s.APIPrefix); p != "" {
		out = append(out, "-apiprefix="+p)
	}
	out = append(out, extra...)
	return out
}

func (s *Session) applyUpdateCLIArgs(target, bundle, agentVariant string, useBundle bool) []string {
	out := s.ginArgs()
	if v := strings.TrimSpace(agentVariant); v != "" {
		out = append(out, "-agent-variant="+v)
	}
	if useBundle {
		out = append(out, "-use-bundle-config")
	}
	out = append(out, target, bundle)
	return out
}

func (s *Session) setCachedDiscovery(list []discovery.DiscoveryResponse) {
	s.LastDiscovery = append([]discovery.DiscoveryResponse(nil), list...)
	s.CachedHosts = discoverycli.BulkPushHostsFromDiscovery(list)
}

func (s *Session) clearHosts() {
	s.LastDiscovery = nil
	s.CachedHosts = nil
}

func (s *Session) requireCachedHosts() ([]clirest.BulkPushHost, error) {
	if len(s.CachedHosts) == 0 {
		return nil, fmt.Errorf("no cached remote hosts; run 'discovery' first")
	}
	out := make([]clirest.BulkPushHost, len(s.CachedHosts))
	copy(out, s.CachedHosts)
	return out, nil
}
