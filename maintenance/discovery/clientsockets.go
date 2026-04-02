package discovery

import (
	"fmt"
	"net"
	"strconv"
	"syscall"
)

// OpenDiscoveryClientUDP binds UDP port srcPort for outbound discovery (CLI).
// When broadcast addresses imply multiple local subnets, one socket per local IP is opened
// (same pattern as the agent when serving HTTP+discovery) so each brd is sent from the correct interface.
// If no subnet match is found or per-IP binds fail, falls back to 0.0.0.0:srcPort.
func OpenDiscoveryClientUDP(srcPort int, broadcastAddrs []string) ([]*net.UDPConn, error) {
	enableBroadcast := func(c *net.UDPConn) {
		raw, err := c.SyscallConn()
		if err != nil {
			return
		}
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
		})
	}

	lips := unionLocalIPsForBroadcastStrings(broadcastAddrs)
	if len(lips) == 0 {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: srcPort})
		if err != nil {
			return nil, err
		}
		enableBroadcast(c)
		return []*net.UDPConn{c}, nil
	}

	var conns []*net.UDPConn
	for _, ip := range lips {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: srcPort})
		if err != nil {
			continue
		}
		enableBroadcast(c)
		conns = append(conns, c)
	}
	if len(conns) == 0 {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: srcPort})
		if err != nil {
			return nil, err
		}
		enableBroadcast(c)
		return []*net.UDPConn{c}, nil
	}
	return conns, nil
}

func unionLocalIPsForBroadcastStrings(broadcastAddrs []string) []net.IP {
	uniq := make(map[string]net.IP)
	for _, s := range broadcastAddrs {
		b := net.ParseIP(s)
		if b == nil {
			continue
		}
		b = b.To4()
		if b == nil {
			continue
		}
		for _, ip := range LocalIPsInSubnet(b) {
			uniq[ip.String()] = ip
		}
	}
	out := make([]net.IP, 0, len(uniq))
	for _, ip := range uniq {
		out = append(out, ip)
	}
	return out
}

// SendDiscoveryClientBroadcast sends the same payload to each broadcast:destPort using the same
// per-interface rules as the agent (LocalIPsInSubnet + matching conn, else conns[0]).
func SendDiscoveryClientBroadcast(conns []*net.UDPConn, payload []byte, destPort int, broadcastAddrs []string) error {
	if len(conns) == 0 {
		return fmt.Errorf("discovery: no UDP sockets")
	}
	for _, brdStr := range broadcastAddrs {
		addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(brdStr, strconv.Itoa(destPort)))
		if err != nil {
			return err
		}
		localIPs := LocalIPsInSubnet(addr.IP)
		if len(localIPs) == 0 {
			if _, err := conns[0].WriteToUDP(payload, addr); err != nil {
				return err
			}
			continue
		}
		seen := make(map[string]bool)
		for _, lip := range localIPs {
			key := lip.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			sent := false
			for _, conn := range conns {
				la, ok := conn.LocalAddr().(*net.UDPAddr)
				if !ok || la == nil || !la.IP.Equal(lip) {
					continue
				}
				if _, err := conn.WriteToUDP(payload, addr); err != nil {
					return err
				}
				sent = true
				break
			}
			if !sent {
				if _, err := conns[0].WriteToUDP(payload, addr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
