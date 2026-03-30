package discoverycli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mol/maintenance/discovery"
	"mol/maintenance/hostinfo"
)

// Run runs standalone UDP discovery (no config file, no HTTP server).
// mol --discovery [--dest-port=N] [--src-port=N] [--timeout=N] [--service=name]
// Returns 0 on success, 1 on error.
func Run(args []string) int {
	fs := flag.NewFlagSet("discovery", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	destPort := fs.Int("dest-port", 9999, "destination UDP port (mol listeners on remote)")
	srcPort := fs.Int("src-port", 9998, "local UDP port to bind (responses arrive here)")
	timeoutSec := fs.Int("timeout", 10, "discovery duration in seconds")
	serviceName := fs.String("service", "mol", "service name in DISCOVERY_REQUEST")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: mol --discovery [flags]\n\n")
		fmt.Fprintf(os.Stderr, "  Sends DISCOVERY_REQUEST to broadcast:<dest-port>, listens on <src-port>.\n\n")
		fs.PrintDefaults()
	}
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fs.Usage()
			return 0
		}
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *destPort <= 0 || *destPort > 65535 {
		fmt.Fprintln(os.Stderr, "mol: --dest-port must be 1..65535")
		return 1
	}
	if *srcPort <= 0 || *srcPort > 65535 {
		fmt.Fprintln(os.Stderr, "mol: --src-port must be 1..65535")
		return 1
	}
	if *timeoutSec <= 0 {
		fmt.Fprintln(os.Stderr, "mol: --timeout must be positive")
		return 1
	}
	svc := strings.TrimSpace(*serviceName)
	if svc == "" {
		svc = "mol"
	}

	broadcastAddrs := hostinfo.GetPhysicalNICBroadcastAddresses()
	if len(broadcastAddrs) == 0 {
		broadcastAddrs = []string{"255.255.255.255"}
	}
	fmt.Println("Discovery brd (broadcast addresses):")
	for _, brd := range broadcastAddrs {
		fmt.Printf("  %s\n", brd)
	}

	conns, err := discovery.OpenDiscoveryClientUDP(*srcPort, broadcastAddrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mol: UDP bind for discovery failed: %v\n", err)
		return 1
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	requestID := discovery.NewRequestID()
	replyUDPPort := *srcPort
	if la, ok := conns[0].LocalAddr().(*net.UDPAddr); ok && la != nil && la.Port > 0 {
		replyUDPPort = la.Port
	}
	req := discovery.DiscoveryRequest{
		Type:         "DISCOVERY_REQUEST",
		Service:      svc,
		RequestID:    requestID,
		ReplyUDPPort: replyUDPPort,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mol:", err)
		return 1
	}
	if err := discovery.ValidateDiscoveryRequestPayload(payload); err != nil {
		fmt.Fprintln(os.Stderr, "mol:", err)
		return 1
	}

	if err := discovery.SendDiscoveryClientBroadcast(conns, payload, *destPort, broadcastAddrs); err != nil {
		fmt.Fprintf(os.Stderr, "mol: discovery broadcast send: %v\n", err)
		return 1
	}

	recvGrace := 500 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second+recvGrace)
	defer cancel()

	var mu sync.Mutex
	var responses []discovery.DiscoveryResponse

	readLoop := func(conn *net.UDPConn) {
		buf := make([]byte, 8192)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
			if resp, ok := discovery.MatchDiscoveryResponseUDP(buf, n, from, requestID, svc); ok {
				mu.Lock()
				responses = append(responses, resp)
				mu.Unlock()
			}
		}
	}
	for _, c := range conns {
		go readLoop(c)
	}

	nw := len(strconv.Itoa(*timeoutSec))
	if nw < 2 {
		nw = 2
	}
	maxLineLen := len(fmt.Sprintf("Discovering ... %*d ", nw, *timeoutSec))
	for i := *timeoutSec; i >= 1; i-- {
		fmt.Printf("\rDiscovering ... %*d ", nw, i)
		time.Sleep(1 * time.Second)
	}
	doneLine := "Discovery Done."
	if len(doneLine) < maxLineLen {
		doneLine = doneLine + strings.Repeat(" ", maxLineLen-len(doneLine))
	}
	fmt.Printf("\r%s\n", doneLine)
	time.Sleep(300 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	drainBuf := make([]byte, 8192)
	for _, conn := range conns {
		_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		for {
			n, from, err := conn.ReadFromUDP(drainBuf)
			if err != nil {
				break
			}
			if resp, ok := discovery.MatchDiscoveryResponseUDP(drainBuf, n, from, requestID, svc); ok {
				mu.Lock()
				responses = append(responses, resp)
				mu.Unlock()
			}
		}
	}

	mu.Lock()
	list := append([]discovery.DiscoveryResponse(nil), responses...)
	mu.Unlock()

	lines := formatResults(list)
	for _, line := range lines {
		fmt.Println(line)
	}
	return 0
}

func formatResults(list []discovery.DiscoveryResponse) []string {
	if len(list) == 0 {
		return []string{"(발견된 호스트 없음)"}
	}
	selfUUID := ""
	if info, err := hostinfo.Get(); err == nil {
		selfUUID = strings.TrimSpace(info.CPUUUID)
	}
	localIPSet := make(map[string]struct{})
	for _, ip := range hostinfo.AllIPv4Addresses() {
		if ip != "" {
			localIPSet[ip] = struct{}{}
		}
	}

	type group struct {
		hostname string
		hostIP   string
		cpuUUID  string
		ips      map[string]struct{}
	}
	groups := make(map[string]*group)
	order := []string{}

	for _, r := range list {
		key := strings.TrimSpace(r.CPUUUID)
		if key == "" {
			hn := strings.TrimSpace(r.Hostname)
			if hn == "" {
				hn = "(이름 없음)"
			}
			key = "noid:" + hn
		}
		g, ok := groups[key]
		if !ok {
			cpu := strings.TrimSpace(r.CPUUUID)
			g = &group{
				hostname: r.Hostname,
				hostIP:   "",
				cpuUUID:  cpu,
				ips:      make(map[string]struct{}),
			}
			if g.hostname == "" {
				g.hostname = "(이름 없음)"
			}
			groups[key] = g
			order = append(order, key)
		} else if g.cpuUUID == "" {
			if u := strings.TrimSpace(r.CPUUUID); u != "" {
				g.cpuUUID = u
			}
		}
		if r.RespondedFromIP != "" {
			g.ips[r.RespondedFromIP] = struct{}{}
			if g.hostIP == "" {
				g.hostIP = r.RespondedFromIP
			}
		}
	}

	out := make([]string, 0, len(order))
	for _, key := range order {
		g := groups[key]
		ipList := make([]string, 0, len(g.ips))
		for ip := range g.ips {
			ipList = append(ipList, ip)
		}
		sort.Strings(ipList)
		primary := g.hostIP
		if primary == "" && len(ipList) > 0 {
			primary = ipList[0]
		}
		tag := localTag(selfUUID, strings.TrimSpace(g.cpuUUID), g.ips, localIPSet)
		out = append(out, fmt.Sprintf("%s %s - %s : [%s]", tag, g.hostname, primary, strings.Join(ipList, ", ")))
	}
	return out
}

func localTag(selfUUID, groupUUID string, responded map[string]struct{}, localIPs map[string]struct{}) string {
	if selfUUID != "" && groupUUID != "" && strings.EqualFold(selfUUID, groupUUID) {
		return "[Local]"
	}
	for ip := range responded {
		if _, ok := localIPs[ip]; ok {
			return "[Local]"
		}
	}
	return "[Remote]"
}
