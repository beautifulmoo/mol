package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HostInfoGetter returns host info for building DISCOVERY_RESPONSE.
type HostInfoGetter func() (hostname, hostIP, cpuInfo string, cpuUsage float64, memTotalMB, memUsedMB uint64, memUsagePct float64, cpuUUID string)

// Config holds discovery-related config.
// DiscoveryBroadcastAddresses must have at least one element.
type Config struct {
	ServiceName                string
	DiscoveryBroadcastAddresses []string // one or more broadcast addresses to send DISCOVERY_REQUEST to
	DiscoveryUDPPort           int
	DiscoveryTimeoutSeconds    int
	DiscoveryDeduplicate       bool
	Version                    string
	ServicePort                int
}

// Discovery handles UDP discovery (listen + respond, and run discovery).
type Discovery struct {
	cfg    Config
	conn   *net.UDPConn
	getter HostInfoGetter

	mu       sync.Mutex
	pending  map[string]chan *DiscoveryResponse
}

// New creates a Discovery. Caller must call conn.ListenUDP or similar and pass the conn.
func New(cfg Config, conn *net.UDPConn, getter HostInfoGetter) *Discovery {
	return &Discovery{
		cfg:     cfg,
		conn:    conn,
		getter:  getter,
		pending: make(map[string]chan *DiscoveryResponse),
	}
}

// Run starts the read loop: handle DISCOVERY_REQUEST (respond) and DISCOVERY_RESPONSE (forward to pending).
func (d *Discovery) Run() {
	buf := make([]byte, 4096)
	for {
		n, from, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "DISCOVERY_REQUEST":
			d.handleRequest(buf[:n], from)
		case "DISCOVERY_RESPONSE":
			d.handleResponse(buf[:n], from)
		}
	}
}

func (d *Discovery) handleRequest(raw []byte, from *net.UDPAddr) {
	var req DiscoveryRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return
	}
	if req.Service != d.cfg.ServiceName {
		return
	}
	log.Printf("discovery: received DISCOVERY_REQUEST from %s", from)
	hostname, hostIP, cpuInfo, cpuUsage, memTotalMB, memUsedMB, memUsagePct, cpuUUID := d.getter()
	// Use outbound IP toward requester so the response has the IP the requester can use to reach us (avoids wrong self-filter when multiple hosts share an IP or getter returns a different interface).
	if out := d.outboundIP(from.IP); out != "" {
		hostIP = out
	}
	resp := DiscoveryResponse{
		Type:               "DISCOVERY_RESPONSE",
		Service:            d.cfg.ServiceName,
		HostIP:             hostIP,
		Hostname:           hostname,
		ServicePort:        d.cfg.ServicePort,
		Version:            d.cfg.Version,
		RequestID:          req.RequestID,
		CPUInfo:            cpuInfo,
		CPUUsagePercent:    cpuUsage,
		CPUUUID:            cpuUUID,
		MemoryTotalMB:      memTotalMB,
		MemoryUsedMB:       memUsedMB,
		MemoryUsagePercent: memUsagePct,
	}
	// Send unicast to from (requester) IP:discovery_port
	to := &net.UDPAddr{IP: from.IP, Port: d.cfg.DiscoveryUDPPort}
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("discovery: failed to marshal DISCOVERY_RESPONSE: %v", err)
		return
	}
	log.Printf("discovery: sending DISCOVERY_RESPONSE to %s (hostname=%s)", to, hostname)
	connOut, err := net.DialUDP("udp", nil, to)
	if err != nil {
		log.Printf("discovery: failed to DialUDP to %s: %v", to, err)
		return
	}
	defer connOut.Close()
	if _, err := connOut.Write(data); err != nil {
		log.Printf("discovery: failed to write DISCOVERY_RESPONSE to %s: %v", to, err)
		return
	}
}

// OutboundIP returns the local IP used when sending to remote:port (e.g. broadcast address). Use for "my IP on this network".
func OutboundIP(remote net.IP, port int) string {
	to := &net.UDPAddr{IP: remote, Port: port}
	conn, err := net.DialUDP("udp", nil, to)
	if err != nil {
		return ""
	}
	defer conn.Close()
	addr := conn.LocalAddr()
	if u, ok := addr.(*net.UDPAddr); ok {
		return u.IP.String()
	}
	return ""
}

func (d *Discovery) outboundIP(remote net.IP) string {
	return OutboundIP(remote, d.cfg.DiscoveryUDPPort)
}

func (d *Discovery) handleResponse(raw []byte, from *net.UDPAddr) {
	var resp DiscoveryResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		log.Printf("discovery: failed to parse DISCOVERY_RESPONSE from %s: %v", from, err)
		return
	}
	d.mu.Lock()
	ch := d.pending[resp.RequestID]
	d.mu.Unlock()
	if ch != nil {
		select {
		case ch <- &resp:
			log.Printf("discovery: received DISCOVERY_RESPONSE from %s requestID=%s hostname=%s (delivered)", from, resp.RequestID, resp.Hostname)
		default:
			log.Printf("discovery: received DISCOVERY_RESPONSE from %s requestID=%s (pending channel full, dropped)", from, resp.RequestID)
		}
	} else {
		log.Printf("discovery: received DISCOVERY_RESPONSE from %s requestID=%s (no pending waiter, stale or unknown)", from, resp.RequestID)
	}
}

// DoDiscovery sends a DISCOVERY_REQUEST to each configured broadcast address and collects responses until timeout. Deduplicates by host_ip:service_port if configured.
func (d *Discovery) DoDiscovery() ([]DiscoveryResponse, error) {
	requestID := newRequestID()
	req := DiscoveryRequest{
		Type:      "DISCOVERY_REQUEST",
		Service:   d.cfg.ServiceName,
		RequestID: requestID,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if len(d.cfg.DiscoveryBroadcastAddresses) == 0 {
		return nil, fmt.Errorf("discovery: no broadcast addresses configured")
	}
	addrs := make([]*net.UDPAddr, 0, len(d.cfg.DiscoveryBroadcastAddresses))
	for _, a := range d.cfg.DiscoveryBroadcastAddresses {
		addr, err := net.ResolveUDPAddr("udp", a+":"+strconv.Itoa(d.cfg.DiscoveryUDPPort))
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	// Register pending before sending so we don't miss fast responses (e.g. self-response or same-LAN reply).
	ch := make(chan *DiscoveryResponse, 32)
	d.mu.Lock()
	d.pending[requestID] = ch
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.pending, requestID)
		d.mu.Unlock()
		close(ch)
	}()
	for _, addr := range addrs {
		if _, err = d.conn.WriteToUDP(data, addr); err != nil {
			return nil, err
		}
		log.Printf("discovery: sent DISCOVERY_REQUEST requestID=%s to %s", requestID, addr)
	}
	timeout := time.Duration(d.cfg.DiscoveryTimeoutSeconds) * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	// Exclude self by CPU UUID so we correctly exclude local host even when it responds with different IPs (e.g. multiple interfaces).
	_, _, _, _, _, _, _, selfCPUUUID := d.getter()
	seen := make(map[string]struct{})
	var list []DiscoveryResponse
	processResponse := func(r *DiscoveryResponse) {
		if selfCPUUUID != "" && r.CPUUUID != "" && r.CPUUUID == selfCPUUUID {
			return
		}
		// Fallback when CPU UUID is not available: exclude by IP + service port.
		if selfCPUUUID == "" {
			selfHostIP := d.outboundIP(addrs[0].IP)
			if selfHostIP == "" {
				_, selfHostIP, _, _, _, _, _, _ = d.getter()
			}
			if selfHostIP != "" && r.ServicePort == d.cfg.ServicePort && r.HostIP == selfHostIP {
				return
			}
		}
		key := r.HostIP + ":" + fmt.Sprint(r.ServicePort)
		if d.cfg.DiscoveryDeduplicate {
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
		}
		list = append(list, *r)
	}

	for {
		select {
		case r, ok := <-ch:
			if !ok {
				return list, nil
			}
			processResponse(r)
		case <-timer.C:
			// Drain channel before returning: select may choose timer when both are ready, so we'd miss responses already in the channel.
			for {
				select {
				case r, ok := <-ch:
					if !ok {
						return list, nil
					}
					processResponse(r)
				default:
					return list, nil
				}
			}
		}
	}
}

// DoDiscoveryStream sends a DISCOVERY_REQUEST to each configured broadcast address and yields each response on the returned channel as it arrives (after self-filter and dedup). The channel is closed when the timeout expires. Caller must consume the channel until closed.
func (d *Discovery) DoDiscoveryStream() (<-chan DiscoveryResponse, error) {
	requestID := newRequestID()
	req := DiscoveryRequest{
		Type:      "DISCOVERY_REQUEST",
		Service:   d.cfg.ServiceName,
		RequestID: requestID,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if len(d.cfg.DiscoveryBroadcastAddresses) == 0 {
		return nil, fmt.Errorf("discovery: no broadcast addresses configured")
	}
	addrs := make([]*net.UDPAddr, 0, len(d.cfg.DiscoveryBroadcastAddresses))
	for _, a := range d.cfg.DiscoveryBroadcastAddresses {
		addr, err := net.ResolveUDPAddr("udp", a+":"+strconv.Itoa(d.cfg.DiscoveryUDPPort))
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, addr)
	}
	ch := make(chan *DiscoveryResponse, 32)
	d.mu.Lock()
	d.pending[requestID] = ch
	d.mu.Unlock()

	out := make(chan DiscoveryResponse, 8)
	go func() {
		defer close(out)
		defer func() {
			d.mu.Lock()
			delete(d.pending, requestID)
			d.mu.Unlock()
		}()

		for _, addr := range addrs {
			if _, err = d.conn.WriteToUDP(data, addr); err != nil {
				return
			}
			log.Printf("discovery: sent DISCOVERY_REQUEST requestID=%s to %s (stream)", requestID, addr)
		}

		timeout := time.Duration(d.cfg.DiscoveryTimeoutSeconds) * time.Second
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		_, _, _, _, _, _, _, selfCPUUUID := d.getter()
		seen := make(map[string]struct{})

		processAndMaybeSend := func(r *DiscoveryResponse) bool {
			if selfCPUUUID != "" && r.CPUUUID != "" && r.CPUUUID == selfCPUUUID {
				return false
			}
			if selfCPUUUID == "" {
				selfHostIP := d.outboundIP(addrs[0].IP)
				if selfHostIP == "" {
					_, selfHostIP, _, _, _, _, _, _ = d.getter()
				}
				if selfHostIP != "" && r.ServicePort == d.cfg.ServicePort && r.HostIP == selfHostIP {
					return false
				}
			}
			key := r.HostIP + ":" + fmt.Sprint(r.ServicePort)
			if d.cfg.DiscoveryDeduplicate {
				if _, ok := seen[key]; ok {
					return false
				}
				seen[key] = struct{}{}
			}
			return true
		}

		for {
			select {
			case r, ok := <-ch:
				if !ok {
					return
				}
				if processAndMaybeSend(r) {
					out <- *r
				}
			case <-timer.C:
				for {
					select {
					case r, ok := <-ch:
						if !ok {
							return
						}
						if processAndMaybeSend(r) {
							out <- *r
						}
					default:
						return
					}
				}
			}
		}
	}()

	return out, nil
}

// DoDiscoveryUnicast sends a DISCOVERY_REQUEST to the given IP (unicast) and returns that host's DiscoveryResponse, or error on timeout/no response.
// The IP should be the host's address; the request is sent to ip:DiscoveryUDPPort.
func (d *Discovery) DoDiscoveryUnicast(ip string) (*DiscoveryResponse, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, fmt.Errorf("host ip required")
	}
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(ip, strconv.Itoa(d.cfg.DiscoveryUDPPort)))
	if err != nil {
		return nil, err
	}
	requestID := newRequestID()
	req := DiscoveryRequest{
		Type:      "DISCOVERY_REQUEST",
		Service:   d.cfg.ServiceName,
		RequestID: requestID,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *DiscoveryResponse, 1)
	d.mu.Lock()
	d.pending[requestID] = ch
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.pending, requestID)
		d.mu.Unlock()
		close(ch)
	}()
	if _, err = d.conn.WriteToUDP(data, addr); err != nil {
		return nil, err
	}
	log.Printf("discovery: sent DISCOVERY_REQUEST requestID=%s to %s (unicast)", requestID, addr)
	timeout := time.Duration(d.cfg.DiscoveryTimeoutSeconds) * time.Second
	if timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("no response from %s", ip)
		}
		if r.HostIP != ip {
			return nil, fmt.Errorf("response from wrong host: %s", r.HostIP)
		}
		return r, nil
	case <-timer.C:
		return nil, fmt.Errorf("timeout waiting for response from %s", ip)
	}
}
