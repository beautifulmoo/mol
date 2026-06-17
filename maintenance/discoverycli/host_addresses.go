package discoverycli

import (
	"strings"

	"contrabass-agent/maintenance/discovery"
)

// HostAddressesForCPUUUID merges host_ip and responded_from_ip from discovery responses for one host.
// primary is the last responded_from_ip seen; connectIP is included in the set and used as primary fallback.
func HostAddressesForCPUUUID(list []discovery.DiscoveryResponse, cpuUUID, connectIP string) (primary string, ips []string) {
	cpuUUID = strings.TrimSpace(cpuUUID)
	connectIP = strings.TrimSpace(connectIP)
	ipSet := make(map[string]struct{})
	var lastResponded string

	for _, r := range list {
		if !discoveryResponseMatchesHost(r, cpuUUID, connectIP) {
			continue
		}
		addDiscoveryResponseIPs(ipSet, r)
		if ip := strings.TrimSpace(r.RespondedFromIP); ip != "" {
			lastResponded = ip
		}
	}
	if connectIP != "" {
		ipSet[connectIP] = struct{}{}
	}

	ips = sortedIPSet(ipSet)

	primary = lastResponded
	if primary == "" && connectIP != "" {
		primary = connectIP
	}
	if primary == "" && len(ips) > 0 {
		primary = ips[0]
	}
	return primary, ips
}

func discoveryResponseMatchesHost(r discovery.DiscoveryResponse, cpuUUID, connectIP string) bool {
	if cpuUUID != "" {
		if u := strings.TrimSpace(r.CPUUUID); u != "" && strings.EqualFold(u, cpuUUID) {
			return true
		}
	}
	if connectIP == "" {
		return false
	}
	if strings.TrimSpace(r.RespondedFromIP) == connectIP {
		return true
	}
	if strings.TrimSpace(r.HostIP) == connectIP {
		return true
	}
	for _, raw := range r.HostIPs {
		if strings.TrimSpace(raw) == connectIP {
			return true
		}
	}
	return false
}
