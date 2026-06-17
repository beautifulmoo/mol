package discoverycli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
)

// Default discovery parameters (match flag defaults on `agent --discovery`).
const (
	DefaultDestPort    = 9999
	DefaultSrcPort     = 9998
	DefaultTimeoutSec  = 10
	DefaultRecvGraceMS = 500
)

// DiscoverOptions configures a standalone UDP discovery run (no HTTP server).
type DiscoverOptions struct {
	DestPort    int
	SrcPort     int
	TimeoutSec  int
	ServiceName string
	// Progress is called once per second with seconds remaining; nil skips countdown UI.
	Progress func(remaining int)
}

// Discover collects DISCOVERY_RESPONSE packets until the timeout elapses.
func Discover(opts DiscoverOptions) ([]discovery.DiscoveryResponse, error) {
	if opts.DestPort <= 0 {
		opts.DestPort = DefaultDestPort
	}
	if opts.SrcPort <= 0 {
		opts.SrcPort = DefaultSrcPort
	}
	if opts.TimeoutSec <= 0 {
		opts.TimeoutSec = DefaultTimeoutSec
	}
	svc := strings.TrimSpace(opts.ServiceName)
	if svc == "" {
		svc = agentcfg.DefaultDiscoveryServiceName
	}

	broadcastAddrs := hostinfo.GetPhysicalNICBroadcastAddresses()
	if len(broadcastAddrs) == 0 {
		broadcastAddrs = []string{"255.255.255.255"}
	}

	conns, err := discovery.OpenDiscoveryClientUDP(opts.SrcPort, broadcastAddrs)
	if err != nil {
		return nil, fmt.Errorf("UDP bind for discovery failed: %w", err)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	requestID := discovery.NewRequestID()
	replyUDPPort := opts.SrcPort
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
		return nil, err
	}
	if err := discovery.ValidateDiscoveryRequestPayload(payload); err != nil {
		return nil, err
	}
	if err := discovery.SendDiscoveryClientBroadcast(conns, payload, opts.DestPort, broadcastAddrs); err != nil {
		return nil, fmt.Errorf("discovery broadcast send: %w", err)
	}

	recvGrace := time.Duration(DefaultRecvGraceMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.TimeoutSec)*time.Second+recvGrace)
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

	if opts.Progress != nil {
		nw := len(strconv.Itoa(opts.TimeoutSec))
		if nw < 2 {
			nw = 2
		}
		for i := opts.TimeoutSec; i >= 1; i-- {
			opts.Progress(i)
			time.Sleep(1 * time.Second)
		}
	} else {
		time.Sleep(time.Duration(opts.TimeoutSec) * time.Second)
	}
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
	defer mu.Unlock()
	return append([]discovery.DiscoveryResponse(nil), responses...), nil
}

// BroadcastAddresses returns discovery brd targets (for CLI status output).
func BroadcastAddresses() []string {
	addrs := hostinfo.GetPhysicalNICBroadcastAddresses()
	if len(addrs) == 0 {
		return []string{"255.255.255.255"}
	}
	return addrs
}
