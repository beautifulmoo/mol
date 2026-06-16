package discoverycli

import (
	"sort"
	"strings"

	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
)

// BulkPushHostsFromDiscovery merges discovery responses into one host per remote (excludes local/self).
func BulkPushHostsFromDiscovery(list []discovery.DiscoveryResponse) []clirest.BulkPushHost {
	selfUUID := ""
	if info, err := hostinfo.Get(); err == nil {
		selfUUID = strings.TrimSpace(info.CPUUUID)
	}
	localIPSet := make(map[string]struct{})
	for _, ip := range hostinfo.AllIPv4Addresses() {
		if ip != "" {
			localIPSet[ip] = struct{}{}
		}
	}

	type group struct {
		hostname string
		cpuUUID  string
		ips      map[string]struct{}
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
		key := strings.TrimSpace(r.CPUUUID)
		if key == "" {
			hn := strings.TrimSpace(r.Hostname)
			if hn == "" {
				hn = "(no name)"
			}
			key = "noid:" + hn
		}
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
		if ip := strings.TrimSpace(r.RespondedFromIP); ip != "" {
			g.ips[ip] = struct{}{}
		}
	}

	out := make([]clirest.BulkPushHost, 0, len(order))
	for _, key := range order {
		g := groups[key]
		ipList := make([]string, 0, len(g.ips))
		for ip := range g.ips {
			ipList = append(ipList, ip)
		}
		sort.Strings(ipList)
		if len(ipList) == 0 {
			continue
		}
		out = append(out, clirest.BulkPushHost{
			PrimaryIP: ipList[0],
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
