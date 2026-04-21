package hostinfoapi

import (
	"contrabass-agent/maintenance/discovery"
)

// RemoteHostInfo runs UDP unicast discovery to ip. Same behavior as GET /host-info?ip=<ip> on the server.
func RemoteHostInfo(d *discovery.Discovery, ip string) (*discovery.DiscoveryResponse, error) {
	return d.DoDiscoveryUnicast(ip)
}
