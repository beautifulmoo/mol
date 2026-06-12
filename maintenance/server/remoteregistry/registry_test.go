package remoteregistry

import (
	"testing"
	"time"

	"contrabass-agent/maintenance/discovery"
)

func sampleDiscovery(ip, uuid string) discovery.DiscoveryResponse {
	return discovery.DiscoveryResponse{
		Type:            "DISCOVERY_RESPONSE",
		HostIP:          ip,
		RespondedFromIP: ip,
		Hostname:        "host-a",
		Version:         "1.0.0",
		BuildVariant:    "compute",
		CPUUUID:         uuid,
		ServicePort:     8889,
	}
}

func TestUpsertFromDiscovery_ignoresSelf(t *testing.T) {
	reg := New(3)
	reg.UpsertFromDiscovery(discovery.DiscoveryResponse{IsSelf: true, HostIP: "10.0.0.1"})
	if reg.Count() != 0 {
		t.Fatalf("count = %d, want 0", reg.Count())
	}
}

func TestUpsertFromDiscovery_keepsDeadOnRediscovery(t *testing.T) {
	reg := New(2)
	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.5", "uuid-1"))
	reg.RecordHealthCheck("10.0.0.5", false, "timeout")
	reg.RecordHealthCheck("10.0.0.5", false, "timeout")

	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.5", "uuid-1"))

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("len = %d", len(list))
	}
	if !list[0].Health.Dead {
		t.Fatal("expected dead health to remain after rediscovery")
	}
	if list[0].Hostname != "host-a" {
		t.Fatalf("hostname = %q", list[0].Hostname)
	}
}

func TestUpsertFromDiscovery_mergeByCPUUUID(t *testing.T) {
	reg := New(3)
	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.5", ""))
	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.5", "uuid-abc"))

	list := reg.List()
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].PrimaryIP != "10.0.0.5" {
		t.Fatalf("primary ip = %q", list[0].PrimaryIP)
	}
	if list[0].CPUUUID != "uuid-abc" {
		t.Fatalf("cpu uuid = %q", list[0].CPUUUID)
	}
}

func TestRecordHealthCheck_successClearsDead(t *testing.T) {
	reg := New(2)
	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.7", "uuid-2"))
	reg.RecordHealthCheck("10.0.0.7", false, "err")
	reg.RecordHealthCheck("10.0.0.7", false, "err")
	if !reg.List()[0].Health.Dead {
		t.Fatal("expected dead")
	}
	reg.RecordHealthCheck("10.0.0.7", true, "")
	r := reg.List()[0]
	if r.Health.Dead || r.Health.ConsecutiveFailures != 0 || !r.Health.OK {
		t.Fatalf("health = %+v", r.Health)
	}
	if r.Health.LastCheckedAt.IsZero() {
		t.Fatal("expected LastCheckedAt")
	}
}

func TestRecordHealthCheck_unknownIPIgnored(t *testing.T) {
	reg := New(3)
	reg.RecordHealthCheck("10.0.0.99", false, "err")
	if reg.Count() != 0 {
		t.Fatalf("count = %d", reg.Count())
	}
}

func TestUpsertFromRemoteIP(t *testing.T) {
	reg := New(3)
	reg.UpsertFromRemoteIP("10.0.0.44")
	if reg.Count() != 1 {
		t.Fatalf("count = %d", reg.Count())
	}
	reg.UpsertFromRemoteIP("")
	reg.UpsertFromRemoteIP("self")
	if reg.Count() != 1 {
		t.Fatalf("count = %d after ignored ips", reg.Count())
	}
}

func TestList_sortedByPrimaryIP(t *testing.T) {
	reg := New(3)
	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.20", "u2"))
	reg.UpsertFromDiscovery(sampleDiscovery("10.0.0.10", "u1"))
	list := reg.List()
	if len(list) != 2 || list[0].PrimaryIP != "10.0.0.10" || list[1].PrimaryIP != "10.0.0.20" {
		t.Fatalf("list order wrong: %+v", list)
	}
	_ = time.Now()
}
