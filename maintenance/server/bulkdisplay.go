package server

import (
	"strings"

	"contrabass-agent/maintenance/server/remoteregistry"
)

func bulkDisplayIP(remote remoteregistry.Remote) string {
	displayIP := strings.TrimSpace(remote.PrimaryIP)
	if displayIP == "" {
		ips := remoteregistry.ConnectIPs(remote)
		if len(ips) > 0 {
			displayIP = ips[0]
		}
	}
	return displayIP
}
