package discoverycli

import (
	"fmt"
	"sort"
	"strings"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
)

func formatResults(list []discovery.DiscoveryResponse) []string {
	if len(list) == 0 {
		return []string{"(no hosts found)"}
	}
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
		hostname     string
		hostIP       string
		cpuUUID      string
		version      string
		buildVariant string
		ips          map[string]struct{}
	}
	groups := make(map[string]*group)
	order := []string{}

	for _, r := range list {
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
			cpu := strings.TrimSpace(r.CPUUUID)
			g = &group{
				hostname:     r.Hostname,
				hostIP:       "",
				cpuUUID:      cpu,
				version:      strings.TrimSpace(r.Version),
				buildVariant: strings.TrimSpace(r.BuildVariant),
				ips:          make(map[string]struct{}),
			}
			if g.hostname == "" {
				g.hostname = "(no name)"
			}
			groups[key] = g
			order = append(order, key)
		} else {
			if g.cpuUUID == "" {
				if u := strings.TrimSpace(r.CPUUUID); u != "" {
					g.cpuUUID = u
				}
			}
			if g.version == "" {
				g.version = strings.TrimSpace(r.Version)
			}
			if g.buildVariant == "" {
				g.buildVariant = strings.TrimSpace(r.BuildVariant)
			}
		}
		if r.RespondedFromIP != "" {
			g.ips[r.RespondedFromIP] = struct{}{}
			if g.hostIP == "" {
				g.hostIP = r.RespondedFromIP
			}
		}
	}

	out := make([]string, 0, len(order))
	for _, key := range order {
		g := groups[key]
		ipList := make([]string, 0, len(g.ips))
		for ip := range g.ips {
			ipList = append(ipList, ip)
		}
		sort.Strings(ipList)
		primary := g.hostIP
		if primary == "" && len(ipList) > 0 {
			primary = ipList[0]
		}
		tag := localTag(selfUUID, strings.TrimSpace(g.cpuUUID), g.ips, localIPSet)
		out = append(out, fmt.Sprintf("%s %s - %s : [%s] version=%s", tag, g.hostname, primary, strings.Join(ipList, ", "), formatDiscoveryVersion(g.version, g.buildVariant)))
	}
	return out
}

func formatDiscoveryVersion(versionKey, buildVariant string) string {
	ver := strings.TrimSpace(versionKey)
	if ver == "" {
		return "?"
	}
	bv := strings.ToLower(strings.TrimSpace(buildVariant))
	if bv == appmeta.AgentVariantControl || bv == appmeta.AgentVariantCompute {
		return ver + " (" + bv + ")"
	}
	return ver
}

func localTag(selfUUID, groupUUID string, responded map[string]struct{}, localIPs map[string]struct{}) string {
	if selfUUID != "" && groupUUID != "" && strings.EqualFold(selfUUID, groupUUID) {
		return "[Local]"
	}
	for ip := range responded {
		if _, ok := localIPs[ip]; ok {
			return "[Local]"
		}
	}
	return "[Remote]"
}
