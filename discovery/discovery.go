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
	conns  []*net.UDPConn // conns[0] = main (:9999), rest = per-localIP (:9999) with SO_REUSEPORT so we can send from each and receive responses on :9999
	getter HostInfoGetter

	mu       sync.Mutex
	pending  map[string]chan *DiscoveryResponse
}

// New creates a Discovery. Caller passes one or more UDP conns (all bound to discovery port, SO_REUSEPORT). conns[0] is the main listener; additional conns allow sending broadcast from each local IP so responses come back to :9999.
func New(cfg Config, conns []*net.UDPConn, getter HostInfoGetter) *Discovery {
	if len(conns) == 0 {
		panic("discovery: at least one conn required")
	}
	return &Discovery{
		cfg:     cfg,
		conns:   conns,
		getter:  getter,
		pending: make(map[string]chan *DiscoveryResponse),
	}
}

// Run starts the read loop: read from all conns, handle DISCOVERY_REQUEST (respond) and DISCOVERY_RESPONSE (forward to pending).
func (d *Discovery) Run() {
	type recv struct {
		data     []byte
		from     *net.UDPAddr
		recvOn   string // which conn received (LocalAddr), for debugging SO_REUSEPORT delivery
		err      error
	}
	ch := make(chan recv, 32)
	for _, c := range d.conns {
		conn := c
		localAddr := conn.LocalAddr().String()
		go func() {
			buf := make([]byte, 4096)
			for {
				n, from, err := conn.ReadFromUDP(buf)
				if err != nil {
					ch <- recv{err: err}
					return
				}
				ch <- recv{data: append([]byte(nil), buf[:n]...), from: from, recvOn: localAddr}
			}
		}()
	}
	for r := range ch {
		if r.err != nil {
			return
		}
		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(r.data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "DISCOVERY_REQUEST":
			d.handleRequest(r.data, r.from)
		case "DISCOVERY_RESPONSE":
			d.handleResponse(r.data, r.from, r.recvOn)
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
	// Send only host_ip (this response's outbound IP toward requester). Requester aggregates IPs from multiple responses (same host, different interfaces).
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

// localIPsInSubnet returns local IPv4 addresses that belong to the same subnet as the given broadcast IP.
// It checks both /24 and /23 so that:
//   - /24: e.g. broadcast 172.29.236.255 → local 172.29.236.x (other system may be 172.29.236.0/24)
//   - /23: e.g. broadcast 172.29.237.255 → local 172.29.236.x and 172.29.237.x (same /23)
// A local IP is included if it matches the broadcast in /24 or in /23, so both network sizes work.
func localIPsInSubnet(broadcast net.IP) []net.IP {
	broadcast = broadcast.To4()
	if broadcast == nil {
		return nil
	}
	mask24 := net.CIDRMask(24, 32)
	mask23 := net.CIDRMask(23, 32)
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil || ipnet.IP.IsLoopback() {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil {
				continue
			}
			// Same /24 or same /23 as broadcast → works for both /24 and /23 networks
			if ip.Mask(mask24).Equal(broadcast.Mask(mask24)) || ip.Mask(mask23).Equal(broadcast.Mask(mask23)) {
				out = append(out, append(net.IP(nil), ip...))
			}
		}
	}
	return out
}

// sendDiscoveryRequest sends data to addr. If localIPs is non-empty, sends from each conn that is bound to one of those IPs (source port stays 9999 so responses are received). Otherwise sends once from d.conns[0].
func (d *Discovery) sendDiscoveryRequest(data []byte, addr *net.UDPAddr, localIPs []net.IP) error {
	if len(localIPs) == 0 {
		_, err := d.conns[0].WriteToUDP(data, addr)
		return err
	}
	seen := make(map[string]bool)
	for _, lip := range localIPs {
		key := lip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		for _, conn := range d.conns {
			la, ok := conn.LocalAddr().(*net.UDPAddr)
			if !ok || la == nil || !la.IP.Equal(lip) {
				continue
			}
			if _, err := conn.WriteToUDP(data, addr); err != nil {
				log.Printf("discovery: send from %s to %s: %v", lip, addr, err)
			}
			break
		}
	}
	return nil
}

func (d *Discovery) handleResponse(raw []byte, from *net.UDPAddr, recvOn string) {
	var resp DiscoveryResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		log.Printf("discovery: failed to parse DISCOVERY_RESPONSE from %s: %v", from, err)
		return
	}
	resp.RespondedFromIP = from.IP.String()
	d.mu.Lock()
	ch := d.pending[resp.RequestID]
	d.mu.Unlock()
	if ch != nil {
		select {
		case ch <- &resp:
			log.Printf("discovery: received DISCOVERY_RESPONSE from %s host_ip=%s recv_on=%s (delivered)", from, resp.HostIP, recvOn)
		default:
			log.Printf("discovery: received DISCOVERY_RESPONSE from %s requestID=%s recv_on=%s (pending channel full, dropped)", from, resp.RequestID, recvOn)
		}
	} else {
		log.Printf("discovery: received DISCOVERY_RESPONSE from %s requestID=%s recv_on=%s (no pending waiter, stale or unknown)", from, resp.RequestID, recvOn)
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
		localIPs := localIPsInSubnet(addr.IP)
		if err = d.sendDiscoveryRequest(data, addr, localIPs); err != nil {
			return nil, err
		}
		if len(localIPs) > 0 {
			log.Printf("discovery: sent DISCOVERY_REQUEST requestID=%s to %s (from %d local IPs)", requestID, addr, len(localIPs))
		} else {
			log.Printf("discovery: sent DISCOVERY_REQUEST requestID=%s to %s", requestID, addr)
		}
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
		if r.RespondedFromIP != "" {
			key = key + "@" + r.RespondedFromIP
		}
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
			localIPs := localIPsInSubnet(addr.IP)
			if err = d.sendDiscoveryRequest(data, addr, localIPs); err != nil {
				return
			}
			if len(localIPs) > 0 {
				log.Printf("discovery: sent DISCOVERY_REQUEST requestID=%s to %s (stream, from %d local IPs)", requestID, addr, len(localIPs))
			} else {
				log.Printf("discovery: sent DISCOVERY_REQUEST requestID=%s to %s (stream)", requestID, addr)
			}
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
			if r.RespondedFromIP != "" {
				key = key + "@" + r.RespondedFromIP
			}
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
					log.Printf("discovery: stream forwarding host %s (hostname=%s) responded_from=%s", r.HostIP, r.Hostname, r.RespondedFromIP)
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
	if _, err = d.conns[0].WriteToUDP(data, addr); err != nil {
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
