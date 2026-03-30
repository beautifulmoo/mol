package discovery

import "fmt"

// MaxDiscoveryRequestPayloadBytes is the exclusive upper bound for DISCOVERY_REQUEST JSON size
// when sent over UDP broadcast. One Ethernet MTU is ~1500 B; after IP/UDP headers, keeping
// the payload strictly below this limit avoids fragmentation issues on typical paths.
const MaxDiscoveryRequestPayloadBytes = 1300

// ValidateDiscoveryRequestPayload returns an error if the marshaled DISCOVERY_REQUEST is
// not strictly smaller than MaxDiscoveryRequestPayloadBytes.
func ValidateDiscoveryRequestPayload(payload []byte) error {
	if len(payload) >= MaxDiscoveryRequestPayloadBytes {
		return fmt.Errorf("discovery: DISCOVERY_REQUEST JSON is %d bytes; must be strictly smaller than %d bytes for broadcast/MTU safety",
			len(payload), MaxDiscoveryRequestPayloadBytes)
	}
	return nil
}
