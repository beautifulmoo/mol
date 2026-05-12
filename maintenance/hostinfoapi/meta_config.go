package hostinfoapi

import (
	"strings"

	"contrabass-agent/maintenance/agentcfg"
)

// SelfMetaFromConfig builds SelfDiscoveryMeta from a loaded config and the running binary's version key (same role as server.Config.Version / Maintenance fields).
func SelfMetaFromConfig(cfg *agentcfg.Config, displayVersion string) SelfDiscoveryMeta {
	dsn := strings.TrimSpace(cfg.DiscoveryServiceName)
	if dsn == "" {
		dsn = agentcfg.DefaultDiscoveryServiceName
	}
	return SelfDiscoveryMeta{
		Version:              displayVersion,
		ServicePort:          effectiveMaintenancePort(cfg),
		DiscoveryServiceName: dsn,
	}
}
