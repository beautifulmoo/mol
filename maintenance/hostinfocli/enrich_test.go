package hostinfocli

import (
	"testing"

	"contrabass-agent/maintenance/discovery"
)

func TestEnrichHostAddresses_usesDiscoveryCache(t *testing.T) {
	cache := []discovery.DiscoveryResponse{
		{CPUUUID: "uuid-a", HostIP: "172.29.236.42", RespondedFromIP: "172.29.237.142"},
	}
	d := discovery.DiscoveryResponse{CPUUUID: "uuid-a", HostIP: "10.0.2.15"}
	enrichHostAddresses(&d, "172.29.236.42", cache)
	if d.HostIP != "172.29.237.142" {
		t.Fatalf("HOST_IP = %q", d.HostIP)
	}
	if len(d.HostIPs) != 2 {
		t.Fatalf("HOST_IPS = %v", d.HostIPs)
	}
}

func TestApplyHostAddressesFromDiscovery_emptyCache(t *testing.T) {
	d := discovery.DiscoveryResponse{CPUUUID: "uuid-a"}
	if applyHostAddressesFromDiscovery(&d, "10.0.0.1", nil) {
		t.Fatal("expected false for empty cache")
	}
}
