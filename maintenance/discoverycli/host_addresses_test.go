package discoverycli

import (
	"testing"

	"contrabass-agent/maintenance/discovery"
)

func TestHostAddressesForCPUUUID_mergesByCPUUUID(t *testing.T) {
	list := []discovery.DiscoveryResponse{
		{CPUUUID: "uuid-a", HostIP: "172.29.236.42", RespondedFromIP: "172.29.237.142"},
		{CPUUUID: "uuid-a", HostIP: "172.29.237.142", RespondedFromIP: "172.29.237.142"},
		{CPUUUID: "uuid-b", HostIP: "10.0.0.9", RespondedFromIP: "10.0.0.9"},
	}
	primary, ips := HostAddressesForCPUUUID(list, "uuid-a", "172.29.236.42")
	if primary != "172.29.237.142" {
		t.Fatalf("primary = %q", primary)
	}
	if len(ips) != 2 {
		t.Fatalf("ips = %v", ips)
	}
}

func TestHostAddressesForCPUUUID_connectIPWhenNoCPUUUID(t *testing.T) {
	list := []discovery.DiscoveryResponse{
		{HostIP: "172.29.236.42", RespondedFromIP: "172.29.237.142"},
	}
	primary, ips := HostAddressesForCPUUUID(list, "", "172.29.236.42")
	if primary != "172.29.237.142" {
		t.Fatalf("primary = %q", primary)
	}
	if len(ips) != 2 {
		t.Fatalf("ips = %v", ips)
	}
}
