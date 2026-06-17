package discoverycli

import (
	"sort"
	"strings"

	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discovery"
)

// BulkPushHostsFromDiscovery merges discovery responses into one host per remote (excludes local/self).
func BulkPushHostsFromDiscovery(list []discovery.DiscoveryResponse) []clirest.BulkPushHost {
	selfUUID, localIPSet := localDiscoveryContext()

	type group struct {
		hostname         string
		cpuUUID          string
		ips              map[string]struct{}
		primaryResponded string
	}
	groups := make(map[string]*group)
	order := []string{}

	for _, r := range list {
		if r.IsSelf {
			continue
		}
		if isLocalDiscoveryResponse(r, selfUUID, localIPSet) {
			continue
		}
		key := discoveryGroupKey(r)
		g, ok := groups[key]
		if !ok {
			g = &group{
				hostname: strings.TrimSpace(r.Hostname),
				cpuUUID:  strings.TrimSpace(r.CPUUUID),
				ips:      make(map[string]struct{}),
			}
			groups[key] = g
			order = append(order, key)
		}
		if g.hostname == "" {
			g.hostname = strings.TrimSpace(r.Hostname)
		}
		if g.cpuUUID == "" {
			g.cpuUUID = strings.TrimSpace(r.CPUUUID)
		}
		addDiscoveryResponseIPs(g.ips, r)
		if ip := strings.TrimSpace(r.RespondedFromIP); ip != "" {
			g.primaryResponded = ip
		}
	}

	out := make([]clirest.BulkPushHost, 0, len(order))
	for _, key := range order {
		g := groups[key]
		ipList := sortedIPSet(g.ips)
		if len(ipList) == 0 {
			continue
		}
		out = append(out, clirest.BulkPushHost{
			PrimaryIP: primaryFromResponded(g.primaryResponded, ipList),
			Hostname:  g.hostname,
			CPUUUID:   g.cpuUUID,
			IPs:       ipList,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PrimaryIP != out[j].PrimaryIP {
			return out[i].PrimaryIP < out[j].PrimaryIP
		}
		return out[i].CPUUUID < out[j].CPUUUID
	})
	return out
}

func isLocalDiscoveryResponse(r discovery.DiscoveryResponse, selfUUID string, localIPs map[string]struct{}) bool {
	cpu := strings.TrimSpace(r.CPUUUID)
	if selfUUID != "" && cpu != "" && strings.EqualFold(selfUUID, cpu) {
		return true
	}
	if ip := strings.TrimSpace(r.RespondedFromIP); ip != "" {
		if _, ok := localIPs[ip]; ok {
			return true
		}
	}
	return false
}
