package hostinfoapi

import (
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/versionsapi"
)

// SelfBuildVariant returns build_variant for self host-info: installed current binary first, else CLI ldflags value.
func SelfBuildVariant(cfg *agentcfg.Config, cliBuildVariant string) string {
	bv := versionsapi.InstalledBuildVariantFromDeploy(versionsapi.DeployRootFromConfig(cfg))
	if bv != "" {
		return bv
	}
	return strings.TrimSpace(cliBuildVariant)
}

// SelfMetaFromConfig builds SelfDiscoveryMeta from a loaded config and the running binary's version key (same role as server.Config.Version / Maintenance fields).
func SelfMetaFromConfig(cfg *agentcfg.Config, displayVersion, buildVariant string) SelfDiscoveryMeta {
	dsn := strings.TrimSpace(cfg.DiscoveryServiceName)
	if dsn == "" {
		dsn = agentcfg.DefaultDiscoveryServiceName
	}
	return SelfDiscoveryMeta{
		Version:              displayVersion,
		ServicePort:          effectiveMaintenancePort(cfg),
		DiscoveryServiceName: dsn,
		BuildVariant:         buildVariant,
	}
}
