package remoteregistry

import (
	"sort"
	"strings"
)

func remoteAllIPs(r Remote) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(ip string) {
		ip = strings.TrimSpace(ip)
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		out = append(out, ip)
	}
	add(r.PrimaryIP)
	add(r.RespondedFromIP)
	add(r.HostIP)
	for _, ip := range r.HostIPs {
		add(ip)
	}
	return out
}

func remotesShareIdentity(a, b Remote) bool {
	u := strings.TrimSpace(a.CPUUUID)
	v := strings.TrimSpace(b.CPUUUID)
	if u != "" && u == v {
		return true
	}
	ha := strings.TrimSpace(a.Hostname)
	hb := strings.TrimSpace(b.Hostname)
	if ha != "" && ha == hb {
		return true
	}
	ipsA := remoteAllIPs(a)
	ipsB := remoteAllIPs(b)
	for _, ipA := range ipsA {
		for _, ipB := range ipsB {
			if ipA == ipB {
				return true
			}
		}
	}
	return false
}

func mergeRemoteGroup(group []Remote) Remote {
	if len(group) == 0 {
		return Remote{}
	}
	best := group[0]
	for _, r := range group[1:] {
		if strings.TrimSpace(r.CPUUUID) != "" && strings.TrimSpace(best.CPUUUID) == "" {
			best = r
			continue
		}
		if r.LastDiscoveredAt.After(best.LastDiscoveredAt) {
			best = r
		}
	}
	seen := make(map[string]bool)
	var mergedIPs []string
	add := func(ip string) {
		ip = strings.TrimSpace(ip)
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		mergedIPs = append(mergedIPs, ip)
	}
	for _, r := range group {
		for _, ip := range remoteAllIPs(r) {
			add(ip)
		}
	}
	best.HostIPs = mergedIPs
	if strings.TrimSpace(best.PrimaryIP) == "" && len(mergedIPs) > 0 {
		best.PrimaryIP = mergedIPs[0]
	}
	if strings.TrimSpace(best.RespondedFromIP) == "" && len(mergedIPs) > 0 {
		best.RespondedFromIP = mergedIPs[0]
	}
	return best
}

func mergeRemotesForPush(all []Remote) []Remote {
	if len(all) == 0 {
		return nil
	}
	parent := make([]int, len(all))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(i, j int) {
		ri, rj := find(i), find(j)
		if ri != rj {
			parent[rj] = ri
		}
	}
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if remotesShareIdentity(all[i], all[j]) {
				union(i, j)
			}
		}
	}
	groups := make(map[int][]Remote)
	for i, r := range all {
		root := find(i)
		groups[root] = append(groups[root], r)
	}
	out := make([]Remote, 0, len(groups))
	for _, g := range groups {
		out = append(out, mergeRemoteGroup(g))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PrimaryIP != out[j].PrimaryIP {
			return out[i].PrimaryIP < out[j].PrimaryIP
		}
		return out[i].CPUUUID < out[j].CPUUUID
	})
	return out
}

// ConnectIPs returns IPs to try for pushing config, in priority order.
func ConnectIPs(r Remote) []string {
	return remoteAllIPs(r)
}
