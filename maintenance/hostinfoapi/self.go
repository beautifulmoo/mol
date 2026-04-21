// Package hostinfoapi holds shared logic for GET /self and GET /host-info (used by server handlers and --host-info CLI).
package hostinfoapi

import (
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
)

// SelfDiscoveryMeta supplies fields for the DISCOVERY-shaped JSON that are not in hostinfo.Info.
type SelfDiscoveryMeta struct {
	Version              string
	ServicePort          int
	DiscoveryServiceName string
}

// SelfDiscoveryResponse returns the same payload shape as GET /self and GET /host-info?ip=self (or empty ip).
func SelfDiscoveryResponse(info hostinfo.Info, meta SelfDiscoveryMeta) discovery.DiscoveryResponse {
	return discovery.DiscoveryResponse{
		Type:                "DISCOVERY_RESPONSE",
		Service:             meta.DiscoveryServiceName,
		HostIP:              info.HostIP,
		HostIPs:             info.HostIPs,
		Hostname:            info.Hostname,
		ServicePort:         meta.ServicePort,
		Version:             meta.Version,
		RequestID:           "",
		CPUInfo:             info.CPUInfo,
		CPUUsagePercent:     info.CPUUsagePercent,
		CPUUUID:             info.CPUUUID,
		MemoryTotalMB:       info.MemoryTotalMB,
		MemoryUsedMB:        info.MemoryUsedMB,
		MemoryUsagePercent:  info.MemoryUsagePercent,
	}
}
