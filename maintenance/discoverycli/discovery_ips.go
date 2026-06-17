package discoverycli

import (
	"strings"

	"contrabass-agent/maintenance/discovery"
)

// addDiscoveryResponseIPs merges addresses from one discovery response into ips.
// Matches web UI "IP" column (host_ip / host_ips per response) plus responded_from_ip when distinct.
func addDiscoveryResponseIPs(ips map[string]struct{}, r discovery.DiscoveryResponse) {
	if ip := strings.TrimSpace(r.HostIP); ip != "" {
		ips[ip] = struct{}{}
	}
	for _, raw := range r.HostIPs {
		if ip := strings.TrimSpace(raw); ip != "" {
			ips[ip] = struct{}{}
		}
	}
	if ip := strings.TrimSpace(r.RespondedFromIP); ip != "" {
		ips[ip] = struct{}{}
	}
}
