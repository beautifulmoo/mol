package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"mol/config"
	"mol/discovery"
	"mol/hostinfo"
	"mol/server"
)

// Version is set at build time: -ldflags "-X main.Version=1.2.3"
var Version string

//go:embed web/*
var webFS embed.FS

func main() {
	cfgPath := ""
	if len(os.Args) > 1 && os.Args[1] == "-config" && len(os.Args) > 2 {
		cfgPath = os.Args[2]
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal("config: ", err)
	}
	version := cfg.Version
	if version == "" {
		version = Version
	}
	if version == "" {
		version = "0.0.0"
	}

	// UDP listener for discovery: one conn on :port (all interfaces) and one per local IPv4 so we can send broadcast from each interface (source port stays 9999 so responses are received).
	portStr := ":" + strconv.Itoa(cfg.DiscoveryUDPPort)
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
				_ = setSOReuseport(int(fd))
			})
		},
	}
	ctx := context.Background()
	pc0, err := lc.ListenPacket(ctx, "udp", portStr)
	if err != nil {
		log.Fatal("listen discovery: ", err)
	}
	conn0 := pc0.(*net.UDPConn)
	defer conn0.Close()
	conns := []*net.UDPConn{conn0}
	seenIP := make(map[string]bool)
	boundIPs := []string{"0.0.0.0"}
	ifaces, _ := net.Interfaces()
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
			ip := ipnet.IP.String()
			if seenIP[ip] {
				continue
			}
			seenIP[ip] = true
			pc, err := lc.ListenPacket(ctx, "udp", net.JoinHostPort(ip, strconv.Itoa(cfg.DiscoveryUDPPort)))
			if err != nil {
				log.Printf("discovery: bind %s:%d failed: %v (responses to this IP may not be received)", ip, cfg.DiscoveryUDPPort, err)
				continue
			}
			conns = append(conns, pc.(*net.UDPConn))
			boundIPs = append(boundIPs, ip)
		}
	}
	log.Printf("discovery: listening on %s (bound IPs: %v)", portStr, boundIPs)
	for i := 1; i < len(conns); i++ {
		defer conns[i].Close()
	}

	getter := func() (hostname, hostIP, cpuInfo string, cpuUsage float64, memTotalMB, memUsedMB uint64, memUsagePct float64, cpuUUID string) {
		info, err := hostinfo.Get()
		if err != nil {
			return "", "", "", 0, 0, 0, 0, ""
		}
		return info.Hostname, info.HostIP, info.CPUInfo, info.CPUUsagePercent,
			info.MemoryTotalMB, info.MemoryUsedMB, info.MemoryUsagePercent, info.CPUUUID
	}

	broadcastAddrs := cfg.DiscoveryBroadcastAddresses
	if len(broadcastAddrs) == 0 {
		if cfg.DiscoveryBroadcastAddress != "" {
			broadcastAddrs = []string{cfg.DiscoveryBroadcastAddress}
		} else {
			broadcastAddrs = []string{"255.255.255.255"}
		}
	}
	discCfg := discovery.Config{
		ServiceName:                 cfg.ServiceName,
		DiscoveryBroadcastAddresses:  broadcastAddrs,
		DiscoveryUDPPort:             cfg.DiscoveryUDPPort,
		DiscoveryTimeoutSeconds:   cfg.DiscoveryTimeoutSeconds,
		DiscoveryDeduplicate:      cfg.DiscoveryDeduplicate,
		Version:                   version,
		ServicePort:               cfg.HTTPPort,
	}
	disc := discovery.New(discCfg, conns, getter)
	go disc.Run()

	// Web FS: embed embeds "web/*" at build time; no web/ dir needed at runtime.
	fsys, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal("web: embedded FS: ", err)
	}
	if _, err := fsys.Open("index.html"); err != nil {
		log.Fatal("web: index.html not in binary (build from repo root with web/ present)")
	}
	getHostInfo := func() (hostinfo.Info, error) {
		info, err := hostinfo.Get()
		if err != nil {
			return info, err
		}
		var addrsForOutbound []string
		if len(cfg.DiscoveryBroadcastAddresses) > 0 {
			addrsForOutbound = cfg.DiscoveryBroadcastAddresses
		} else if cfg.DiscoveryBroadcastAddress != "" {
			addrsForOutbound = []string{cfg.DiscoveryBroadcastAddress}
		}
		seen := make(map[string]struct{})
		for _, a := range addrsForOutbound {
			if ip := net.ParseIP(a); ip != nil {
				if addr := discovery.OutboundIP(ip, cfg.DiscoveryUDPPort); addr != "" {
					if _, ok := seen[addr]; !ok {
						seen[addr] = struct{}{}
						info.HostIPs = append(info.HostIPs, addr)
					}
				}
			}
		}
		if len(info.HostIPs) > 0 {
			info.HostIP = info.HostIPs[0]
		} else if len(addrsForOutbound) > 0 {
			if ip := net.ParseIP(addrsForOutbound[0]); ip != nil {
				if addr := discovery.OutboundIP(ip, cfg.DiscoveryUDPPort); addr != "" {
					info.HostIP = addr
				}
			}
		}
		return info, nil
	}
	srv := server.New(server.Config{
		WebPrefix:            cfg.WebPrefix,
		APIPrefix:            cfg.APIPrefix,
		WebFS:                fsys,
		Discovery:            disc,
		GetHostInfo:          getHostInfo,
		Version:              version,
		ServicePort:          cfg.HTTPPort,
		ServiceName:          cfg.ServiceName,
		SystemctlServiceName: cfg.SystemctlServiceName,
		DeployBase:           cfg.DeployBase,
	})

	listenAddr := ":" + strconv.Itoa(cfg.HTTPPort)
	httpSrv := &http.Server{Addr: listenAddr, Handler: srv.Handler()}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal("http listen: ", err)
	}
	go func() {
		if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigChan
	log.Printf("received %v, shutting down...", sig)

	conn0.Close() // stop discovery Run() and any pending DoDiscovery
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	log.Println("mol stopped")
}
