package discoverycli

import (
	"fmt"
	"strings"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
)

// FormatDiscoveryResultLines formats grouped discovery results for display (CLI/REPL).
func FormatDiscoveryResultLines(list []discovery.DiscoveryResponse) []string {
	return formatResults(list)
}

func formatResults(list []discovery.DiscoveryResponse) []string {
	if len(list) == 0 {
		return []string{"(no hosts found)"}
	}
	selfUUID, localIPSet := localDiscoveryContext()

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
		key := discoveryGroupKey(r)
		g, ok := groups[key]
		if !ok {
			cpu := strings.TrimSpace(r.CPUUUID)
			g = &group{
				hostname:     r.Hostname,
				cpuUUID:      cpu,
				version:      strings.TrimSpace(r.Version),
				buildVariant: strings.TrimSpace(r.BuildVariant),
				ips:          make(map[string]struct{}),
			}
			if strings.TrimSpace(g.hostname) == "" {
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
		addDiscoveryResponseIPs(g.ips, r)
		if ip := strings.TrimSpace(r.RespondedFromIP); ip != "" {
			g.hostIP = ip
		}
	}

	out := make([]string, 0, len(order))
	for _, key := range order {
		g := groups[key]
		ipList := sortedIPSet(g.ips)
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
