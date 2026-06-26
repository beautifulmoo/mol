package server

import (
	"testing"

	"contrabass-agent/maintenance/server/remoteregistry"
)

func TestPushHostInputToRemote_mergesIPs(t *testing.T) {
	r := pushHostInputToRemote(pushHostInput{
		PrimaryIP: "172.29.237.142",
		Hostname:  "kt-vm",
		CPUUUID:   "uuid-1",
		IPs:       []string{"172.29.236.42", "172.29.237.142"},
	})
	ips := remoteregistry.ConnectIPs(r)
	if len(ips) != 2 {
		t.Fatalf("ips = %v", ips)
	}
	if r.Hostname != "kt-vm" {
		t.Fatalf("hostname = %q", r.Hostname)
	}
}

func TestRemotesForConfigPush_usesUIHosts(t *testing.T) {
	s := &Server{
		deployBase:     t.TempDir(),
		remoteRegistry: remoteregistry.New(3),
	}
	hosts := []pushHostInput{{
		PrimaryIP: "172.29.237.142",
		Hostname:  "kt-vm",
		CPUUUID:   "uuid-1",
		IPs:       []string{"172.29.236.42", "172.29.237.142"},
	}}
	// Stale second registry row that would duplicate push without UI hosts.
	s.remoteRegistry.UpsertFromRemoteIP("172.29.236.42")

	remotes := s.bulkRemoteHosts(hosts, nil)
	if len(remotes) != 1 {
		t.Fatalf("len = %d, want 1", len(remotes))
	}
}
