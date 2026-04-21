package hostinfoapi

import (
	"contrabass-agent/maintenance/hostinfo"
)

// LocalSelfInfo returns host information for this machine for CLI / offline use.
// It fills HostIPs from all non-loopback IPv4 addresses (sorted) and sets HostIP to the first,
// approximating the server's getHostInfo enrichment when discovery UDP binds are not available.
func LocalSelfInfo() (hostinfo.Info, error) {
	info, err := hostinfo.Get()
	if err != nil {
		return info, err
	}
	ips := hostinfo.AllIPv4Addresses()
	if len(ips) > 0 {
		info.HostIPs = ips
		info.HostIP = ips[0]
	}
	return info, nil
}
