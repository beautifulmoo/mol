package hostinfocli

import (
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/discoverycli"
)

// UDP discovery wait when enriching host-info IP columns (shorter than `agent --discovery`).
const hostInfoDiscoveryTimeoutSec = 3

func enrichHostAddresses(d *discovery.DiscoveryResponse, target string, discoveryCache []discovery.DiscoveryResponse) {
	if d == nil {
		return
	}
	connectIP := ""
	if !clirest.IsLocalTarget(target) {
		connectIP = strings.TrimSpace(target)
	}

	if applyHostAddressesFromDiscovery(d, connectIP, discoveryCache) {
		return
	}

	list, err := discoverycli.Discover(discoverycli.DiscoverOptions{
		DestPort:    discoverycli.DefaultDestPort,
		SrcPort:     discoverycli.DefaultSrcPort,
		TimeoutSec:  hostInfoDiscoveryTimeoutSec,
		ServiceName: agentcfg.DefaultDiscoveryServiceName,
	})
	if err != nil {
		applyHostInfoIPFallback(d, connectIP)
		return
	}

	if !applyHostAddressesFromDiscovery(d, connectIP, list) {
		applyHostInfoIPFallback(d, connectIP)
	}
}

func applyHostAddressesFromDiscovery(d *discovery.DiscoveryResponse, connectIP string, list []discovery.DiscoveryResponse) bool {
	if d == nil || len(list) == 0 {
		return false
	}
	primary, ips := discoverycli.HostAddressesForCPUUUID(list, d.CPUUUID, connectIP)
	if len(ips) == 0 {
		return false
	}
	d.HostIP = primary
	d.HostIPs = ips
	d.RespondedFromIP = primary
	return true
}

func applyHostInfoIPFallback(d *discovery.DiscoveryResponse, connectIP string) {
	primary, ips := discoverycli.FallbackHostIPs(connectIP, *d)
	if primary != "" {
		d.HostIP = primary
		d.RespondedFromIP = primary
	}
	if len(ips) > 0 {
		d.HostIPs = ips
	}
}
