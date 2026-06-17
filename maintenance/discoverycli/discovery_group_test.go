package discoverycli

import (
	"testing"

	"contrabass-agent/maintenance/discovery"
)

func TestParseDiscoverArgs_defaults(t *testing.T) {
	opts, err := ParseDiscoverArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.DestPort != DefaultDestPort || opts.SrcPort != DefaultSrcPort {
		t.Fatalf("ports = %d/%d", opts.DestPort, opts.SrcPort)
	}
}

func TestFallbackHostIPs_prefersConnectIP(t *testing.T) {
	d := discovery.DiscoveryResponse{HostIP: "10.0.0.5", RespondedFromIP: "10.0.0.9"}
	primary, ips := FallbackHostIPs("172.29.1.1", d)
	if primary != "172.29.1.1" {
		t.Fatalf("primary = %q", primary)
	}
	if len(ips) < 2 {
		t.Fatalf("ips = %v", ips)
	}
}
