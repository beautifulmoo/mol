package hostinfoapi

import (
	"fmt"
	"net"
	"strings"
	"syscall"

	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
)

// StartEphemeralDiscovery opens a single UDP socket on srcPort (0.0.0.0:srcPort), starts discovery.Run,
// and returns a Discovery usable for DoDiscoveryUnicast to remote ip:cfg.DiscoveryUDPPort.
//
// We intentionally do not use discovery.OpenDiscoveryClientUDP here: that helper opens one socket per
// local subnet for broadcast sourcing; map iteration makes conns[0] non-deterministic, while
// DoDiscoveryUnicast only sends via conns[0], which can cause intermittent timeouts. Unicast-only
// clients need one listener matching reply_udp_port (same as discovery CLI fallback when no subnet match).
// Default srcPort should be 9998 when the agent uses 9999. Call cleanup to close the socket.
func StartEphemeralDiscovery(cfg *config.Config, displayVersion string, srcPort int) (*discovery.Discovery, func(), error) {
	if cfg.DiscoveryUDPPort <= 0 || cfg.DiscoveryUDPPort > 65535 {
		return nil, nil, fmt.Errorf("DiscoveryUDPPort must be 1..65535")
	}
	if srcPort <= 0 || srcPort > 65535 {
		return nil, nil, fmt.Errorf("srcPort must be 1..65535")
	}

	broadcastAddrs := discoveryBroadcastAddrs(cfg)
	conn, err := openUnicastClientUDP(srcPort)
	if err != nil {
		return nil, nil, err
	}
	conns := []*net.UDPConn{conn}

	dsn := strings.TrimSpace(cfg.DiscoveryServiceName)
	if dsn == "" {
		dsn = config.DefaultDiscoveryServiceName
	}
	discCfg := discovery.Config{
		DiscoveryServiceName:        dsn,
		DiscoveryBroadcastAddresses: broadcastAddrs,
		DiscoveryUDPPort:            cfg.DiscoveryUDPPort,
		DiscoveryTimeoutSeconds:     cfg.DiscoveryTimeoutSeconds,
		DiscoveryDeduplicate:        cfg.DiscoveryDeduplicate,
		Version:                     displayVersion,
		ServicePort:                 effectiveMaintenancePort(cfg),
	}

	getter := func() (hostname, hostIP, cpuInfo string, cpuUsage float64, memTotalMB, memUsedMB uint64, memUsagePct float64, cpuUUID string) {
		info, err := hostinfo.Get()
		if err != nil {
			return "", "", "", 0, 0, 0, 0, ""
		}
		return info.Hostname, info.HostIP, info.CPUInfo, info.CPUUsagePercent,
			info.MemoryTotalMB, info.MemoryUsedMB, info.MemoryUsagePercent, info.CPUUUID
	}

	d := discovery.New(discCfg, conns, getter)
	go d.Run()

	cleanup := func() { _ = conn.Close() }
	return d, cleanup, nil
}

func openUnicastClientUDP(srcPort int) (*net.UDPConn, error) {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: srcPort})
	if err != nil {
		return nil, err
	}
	raw, err := c.SyscallConn()
	if err == nil {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
		})
	}
	return c, nil
}

func discoveryBroadcastAddrs(cfg *config.Config) []string {
	addrs := hostinfo.GetPhysicalNICBroadcastAddresses()
	if len(addrs) == 0 {
		if cfg.DiscoveryBroadcastAddress != "" {
			return []string{cfg.DiscoveryBroadcastAddress}
		}
		return []string{"255.255.255.255"}
	}
	return addrs
}

func effectiveMaintenancePort(cfg *config.Config) int {
	p := cfg.MaintenancePort
	if p <= 0 {
		return 8889
	}
	return p
}
