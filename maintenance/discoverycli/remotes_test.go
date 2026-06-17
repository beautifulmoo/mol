package discoverycli

import (
	"strings"
	"testing"

	"contrabass-agent/maintenance/discovery"
)

func TestBulkPushHostsFromDiscovery_excludesSelfAndMergesIPs(t *testing.T) {
	list := []discovery.DiscoveryResponse{
		{IsSelf: true, Hostname: "local", RespondedFromIP: "10.0.0.1"},
		{Hostname: "node-a", CPUUUID: "uuid-a", HostIP: "10.0.0.2", RespondedFromIP: "10.0.0.2"},
		{Hostname: "node-a", CPUUUID: "uuid-a", HostIP: "10.0.0.3", RespondedFromIP: "10.0.0.2"},
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
	if hosts[0].PrimaryIP != "10.0.0.2" {
		t.Fatalf("primary = %q, want responded_from", hosts[0].PrimaryIP)
	}
}

func TestFormatDiscoveryResultLines_mergesHostIPAndRespondedFrom(t *testing.T) {
	list := []discovery.DiscoveryResponse{
		{Hostname: "node-a", CPUUUID: "uuid-a", HostIP: "172.29.236.42", RespondedFromIP: "172.29.237.142", Version: "1.0", BuildVariant: "control"},
	}
	lines := FormatDiscoveryResultLines(list)
	if len(lines) != 1 {
		t.Fatalf("len = %d", len(lines))
	}
	line := lines[0]
	if !strings.Contains(line, "172.29.236.42") || !strings.Contains(line, "172.29.237.142") {
		t.Fatalf("line = %q, want both IPs in brackets", line)
	}
	if !strings.Contains(line, "node-a - 172.29.237.142") {
		t.Fatalf("line = %q, want primary responded_from", line)
	}
}
