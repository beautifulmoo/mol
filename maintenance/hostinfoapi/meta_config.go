package hostinfoapi

import (
	"strings"

	"contrabass-agent/internal/config"
)

// SelfMetaFromConfig builds SelfDiscoveryMeta from a loaded config and the running binary's version key (same role as server.Config.Version / Maintenance fields).
func SelfMetaFromConfig(cfg *config.Config, displayVersion string) SelfDiscoveryMeta {
	dsn := strings.TrimSpace(cfg.DiscoveryServiceName)
	if dsn == "" {
		dsn = config.DefaultDiscoveryServiceName
	}
	return SelfDiscoveryMeta{
		Version:              displayVersion,
		ServicePort:          effectiveMaintenancePort(cfg),
		DiscoveryServiceName: dsn,
	}
}
