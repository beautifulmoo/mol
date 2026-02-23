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

	// UDP listener for discovery (receive requests + receive responses)
	addr, err := net.ResolveUDPAddr("udp", ":"+strconv.Itoa(cfg.DiscoveryUDPPort))
	if err != nil {
		log.Fatal("resolve discovery addr: ", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal("listen discovery: ", err)
	}
	defer conn.Close()
	// Allow sending to broadcast address (required for discovery request).
	if raw, err := conn.SyscallConn(); err == nil {
		raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
		})
	}

	getter := func() (hostname, hostIP, cpuInfo string, cpuUsage float64, memTotalMB, memUsedMB uint64, memUsagePct float64) {
		info, err := hostinfo.Get()
		if err != nil {
			return "", "", "", 0, 0, 0, 0
		}
		return info.Hostname, info.HostIP, info.CPUInfo, info.CPUUsagePercent,
			info.MemoryTotalMB, info.MemoryUsedMB, info.MemoryUsagePercent
	}

	discCfg := discovery.Config{
		ServiceName:               cfg.ServiceName,
		DiscoveryBroadcastAddress: cfg.DiscoveryBroadcastAddress,
		DiscoveryUDPPort:          cfg.DiscoveryUDPPort,
		DiscoveryTimeoutSeconds:   cfg.DiscoveryTimeoutSeconds,
		DiscoveryDeduplicate:      cfg.DiscoveryDeduplicate,
		Version:                   version,
		ServicePort:               cfg.HTTPPort,
	}
	disc := discovery.New(discCfg, conn, getter)
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
		if ip := net.ParseIP(cfg.DiscoveryBroadcastAddress); ip != nil {
			if addr := discovery.OutboundIP(ip, cfg.DiscoveryUDPPort); addr != "" {
				info.HostIP = addr
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
		SSHUser:              cfg.SSHUser,
		SSHIdentityFile:     cfg.SSHIdentityFile,
		DeployBase:          cfg.DeployBase,
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

	conn.Close() // stop discovery Run() and any pending DoDiscovery
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	log.Println("mol stopped")
}
