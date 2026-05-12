package hostinfoapi

import (
	"strings"

	"contrabass-agent/maintenance/molcfg"
)

// SelfMetaFromConfig builds SelfDiscoveryMeta from a loaded config and the running binary's version key (same role as server.Config.Version / Maintenance fields).
func SelfMetaFromConfig(cfg *molcfg.Config, displayVersion string) SelfDiscoveryMeta {
	dsn := strings.TrimSpace(cfg.DiscoveryServiceName)
	if dsn == "" {
		dsn = molcfg.DefaultDiscoveryServiceName
	}
	return SelfDiscoveryMeta{
		Version:              displayVersion,
		ServicePort:          effectiveMaintenancePort(cfg),
		DiscoveryServiceName: dsn,
	}
}
