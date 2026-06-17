package discoverycli

import (
	"sort"
	"strings"

	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
)

func discoveryGroupKey(r discovery.DiscoveryResponse) string {
	key := strings.TrimSpace(r.CPUUUID)
	if key != "" {
		return key
	}
	hn := strings.TrimSpace(r.Hostname)
	if hn == "" {
		hn = "(no name)"
	}
	return "noid:" + hn
}

func localDiscoveryContext() (selfUUID string, localIPSet map[string]struct{}) {
	localIPSet = make(map[string]struct{})
	if info, err := hostinfo.Get(); err == nil {
		selfUUID = strings.TrimSpace(info.CPUUUID)
	}
	for _, ip := range hostinfo.AllIPv4Addresses() {
		if ip != "" {
			localIPSet[ip] = struct{}{}
		}
	}
	return selfUUID, localIPSet
}

func sortedIPSet(ips map[string]struct{}) []string {
	if len(ips) == 0 {
		return nil
	}
	out := make([]string, 0, len(ips))
	for ip := range ips {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

func primaryFromResponded(lastResponded string, ipList []string) string {
	if ip := strings.TrimSpace(lastResponded); ip != "" {
		return ip
	}
	if len(ipList) > 0 {
		return ipList[0]
	}
	return ""
}

// FallbackHostIPs builds HOST_IP/HOST_IPS when discovery enrichment is unavailable.
// connectIP is preferred as primary (host-info CLI target).
func FallbackHostIPs(connectIP string, d discovery.DiscoveryResponse) (primary string, ips []string) {
	ipSet := make(map[string]struct{})
	if ip := strings.TrimSpace(connectIP); ip != "" {
		ipSet[ip] = struct{}{}
	}
	addDiscoveryResponseIPs(ipSet, d)
	ips = sortedIPSet(ipSet)
	primary = strings.TrimSpace(connectIP)
	if primary == "" {
		primary = strings.TrimSpace(d.RespondedFromIP)
	}
	if primary == "" && len(ips) > 0 {
		primary = ips[0]
	}
	return primary, ips
}
