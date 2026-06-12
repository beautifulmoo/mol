package remoteregistry

import "testing"

func TestMergeRemotesForPush_sameHostTwoIPs(t *testing.T) {
	all := []Remote{
		{PrimaryIP: "10.0.0.1", HostIP: "10.0.0.2", CPUUUID: "uuid-1", Hostname: "a"},
		{PrimaryIP: "10.0.0.2", HostIP: "10.0.0.2", CPUUUID: "uuid-1", Hostname: "a"},
	}
	merged := mergeRemotesForPush(all)
	if len(merged) != 1 {
		t.Fatalf("len = %d, want 1", len(merged))
	}
	ips := ConnectIPs(merged[0])
	if len(ips) < 2 {
		t.Fatalf("connect ips = %v", ips)
	}
}

func TestMergeRemotesForPush_overlapWithoutUUID(t *testing.T) {
	all := []Remote{
		{PrimaryIP: "172.29.236.41", HostIP: "172.29.236.41"},
		{PrimaryIP: "172.29.237.141", HostIP: "172.29.237.141", RespondedFromIP: "172.29.236.41"},
	}
	merged := mergeRemotesForPush(all)
	if len(merged) != 1 {
		t.Fatalf("len = %d, want 1", len(merged))
	}
}

func TestMergeRemotesForPush_sameHostname(t *testing.T) {
	all := []Remote{
		{PrimaryIP: "172.29.236.42", Hostname: "kt-vm"},
		{PrimaryIP: "172.29.237.142", Hostname: "kt-vm"},
	}
	merged := mergeRemotesForPush(all)
	if len(merged) != 1 {
		t.Fatalf("len = %d, want 1", len(merged))
	}
}

func TestListForPush(t *testing.T) {
	reg := New(3)
	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.5", "uuid-1"))
	reg.UpsertFromRemoteIP("10.0.0.5")
	if len(reg.ListForPush()) != 1 {
		t.Fatalf("ListForPush len = %d", len(reg.ListForPush()))
	}
}
