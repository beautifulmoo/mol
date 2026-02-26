package discovery

// DiscoveryRequest is sent to broadcast address (UDP).
type DiscoveryRequest struct {
	Type      string `json:"type"` // "DISCOVERY_REQUEST"
	Service   string `json:"service"`
	RequestID string `json:"request_id"`
}

// DiscoveryResponse is sent unicast to requester IP:discovery_port.
type DiscoveryResponse struct {
	Type                string   `json:"type"` // "DISCOVERY_RESPONSE"
	Service             string   `json:"service"`
	HostIP              string   `json:"host_ip"`
	HostIPs             []string `json:"host_ips,omitempty"` // optional; for self, all IPs this host responds with (e.g. per broadcast)
	Hostname            string   `json:"hostname"`
	ServicePort         int     `json:"service_port"`
	Version             string  `json:"version"`
	RequestID           string  `json:"request_id"`
	CPUInfo             string  `json:"cpu_info"`
	CPUUsagePercent     float64 `json:"cpu_usage_percent"`
	CPUUUID             string  `json:"cpu_uuid"`
	MemoryTotalMB       uint64  `json:"memory_total_mb"`
	MemoryUsedMB        uint64  `json:"memory_used_mb"`
	MemoryUsagePercent  float64 `json:"memory_usage_percent"`
	// RespondedFromIP is set by the receiver: UDP source IP of the packet (the IP that actually sent this response). Not sent over the wire.
	RespondedFromIP string `json:"responded_from_ip,omitempty"`
}
