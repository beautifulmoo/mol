package discovery

import (
	"net"
	"testing"
)

func TestMatchDiscoveryResponseUDP(t *testing.T) {
	from := &net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 9999}
	buf := []byte(`{"type":"DISCOVERY_RESPONSE","service":"mol","request_id":"abc","host_ip":"10.0.0.1","hostname":"h"}`)
	_, ok := MatchDiscoveryResponseUDP(buf, len(buf), from, "x", "mol")
	if ok {
		t.Fatal("expected no match for wrong request id")
	}
	r, ok := MatchDiscoveryResponseUDP(buf, len(buf), from, "abc", "mol")
	if !ok {
		t.Fatal("expected match")
	}
	if r.RespondedFromIP != "10.0.0.5" {
		t.Fatalf("RespondedFromIP: got %q", r.RespondedFromIP)
	}
}
