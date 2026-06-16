package discoverycli

import (
	"testing"

	"contrabass-agent/maintenance/discovery"
)

func TestBulkPushHostsFromDiscovery_excludesSelfAndMergesIPs(t *testing.T) {
	list := []discovery.DiscoveryResponse{
		{IsSelf: true, Hostname: "local", RespondedFromIP: "10.0.0.1"},
		{Hostname: "node-a", CPUUUID: "uuid-a", RespondedFromIP: "10.0.0.2"},
		{Hostname: "node-a", CPUUUID: "uuid-a", RespondedFromIP: "10.0.0.3"},
	}
	hosts := BulkPushHostsFromDiscovery(list)
	if len(hosts) != 1 {
		t.Fatalf("len = %d, want 1", len(hosts))
	}
	if hosts[0].CPUUUID != "uuid-a" {
		t.Fatalf("cpu_uuid = %q", hosts[0].CPUUUID)
	}
	if len(hosts[0].IPs) != 2 {
		t.Fatalf("ips = %v", hosts[0].IPs)
	}
}
