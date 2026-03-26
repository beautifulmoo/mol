package discovery

// DiscoveryRequest is sent to broadcast address (UDP).
type DiscoveryRequest struct {
	Type      string `json:"type"` // "DISCOVERY_REQUEST"
	Service   string `json:"service"`
	RequestID string `json:"request_id"`
	// ReplyUDPPort is the UDP port the responder must send DISCOVERY_RESPONSE to (requester's listen port).
	// When set (>0), it overrides the packet's source port so discovery works even if from.Port is wrong or 0.
	ReplyUDPPort int `json:"reply_udp_port,omitempty"`
}

// DiscoveryResponse is sent unicast to requester IP:reply port (UDP source port or reply_udp_port from request).
type DiscoveryResponse struct {
	Type                string   `json:"type"` // "DISCOVERY_RESPONSE"
	Service             string   `json:"service"`
	HostIP              string   `json:"host_ip"`
	HostIPs             []string `json:"host_ips,omitempty"` // HTTP /self 등에서만 채움; UDP Discovery 응답에는 넣지 않음(접속 가능 IP는 responded_from_ip)
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
	// IsSelf is set when the response is from this host (CPU UUID match). Stream receiver uses it to update the self card's "응답한 IP" only.
	IsSelf bool `json:"self,omitempty"`
}
