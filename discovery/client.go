package discovery

import (
	"encoding/json"
	"net"
)

// MatchDiscoveryResponseUDP parses buf[:n] as JSON; if it is a DISCOVERY_RESPONSE
// matching requestID and service, returns (resp, true) with RespondedFromIP set from from.
func MatchDiscoveryResponseUDP(buf []byte, n int, from *net.UDPAddr, requestID, service string) (DiscoveryResponse, bool) {
	if n <= 0 || from == nil {
		return DiscoveryResponse{}, false
	}
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(buf[:n], &head) != nil || head.Type != "DISCOVERY_RESPONSE" {
		return DiscoveryResponse{}, false
	}
	var resp DiscoveryResponse
	if json.Unmarshal(buf[:n], &resp) != nil {
		return DiscoveryResponse{}, false
	}
	if resp.RequestID != requestID || resp.Service != service {
		return DiscoveryResponse{}, false
	}
	resp.RespondedFromIP = from.IP.String()
	return resp, true
}
