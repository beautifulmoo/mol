package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"mol/internal/config"
	"mol/maintenance/discovery"
	"mol/maintenance/hostinfo"
	"mol/maintenance/server"
)

// Version is set at build time: -ldflags "-X main.Version=1.2.3"
var Version string

const helpText = `mol — Discovery 및 웹 UI가 있는 mol 서비스

사용법:
  mol -config <파일>     설정 파일을 지정해야 HTTP 서버 + Discovery가 시작됩니다 (필수)
  mol                    인자 없이 실행 시 버전 안내 후 종료 (서비스는 시작하지 않음)

옵션:
  -h, --help             이 도움말 출력
  -version, --version    버전 출력 후 종료
  --nic-brd              물리 NIC별 IPv4 브로드캐스트(brd) 주소 출력 (Discovery용 확인) 후 종료
  --discovery [flags]    설정 없이 UDP Discovery만 수행 (mol --discovery -h)

`

//go:embed maintenance/web/*
var webFS embed.FS

func molVersionLine() string {
	v := Version
	if v == "" {
		v = "devel"
	}
	return "mol " + v
}

func printMustSpecifyConfig() {
	fmt.Println(molVersionLine())
	fmt.Println()
	fmt.Println("HTTP 서비스와 Discovery를 시작하려면 설정 파일을 지정하세요.")
	fmt.Println("  mol -config <config.yaml>")
	fmt.Println()
	fmt.Println("자세한 옵션은 다음을 실행하세요.")
	fmt.Println("  mol -h")
	fmt.Println("  mol --help")
}

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Print(helpText)
			os.Exit(0)
		case "--version", "-version":
			fmt.Println(molVersionLine())
			os.Exit(0)
		case "--nic-brd":
			pairs := hostinfo.GetPhysicalNICBrdPairs()
			for _, p := range pairs {
				fmt.Printf("%s : %s\n", p.Iface, p.Brd)
			}
			os.Exit(0)
		case "--discovery":
			runDiscoveryCLI(os.Args[2:])
			os.Exit(0)
		}
	}

	if len(os.Args) == 1 {
		printMustSpecifyConfig()
		os.Exit(0)
	}
	if os.Args[1] != "-config" {
		fmt.Fprintf(os.Stderr, "알 수 없는 인자: %q\n\n", os.Args[1])
		printMustSpecifyConfig()
		os.Exit(1)
	}
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "mol: -config 다음에 설정 파일 경로가 필요합니다.")
		fmt.Fprintln(os.Stderr, "예: mol -config /opt/mol/config.yaml")
		os.Exit(1)
	}
	cfgPath := os.Args[2]
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
	// udp4 keeps IPv4 sockaddr handling consistent with discovery CLI and reply_udp_port handling.
	pc0, err := lc.ListenPacket(ctx, "udp4", portStr)
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
			pc, err := lc.ListenPacket(ctx, "udp4", net.JoinHostPort(ip, strconv.Itoa(cfg.DiscoveryUDPPort)))
			if err != nil {
				log.Printf("discovery: bind %s:%d failed: %v (responses to this IP may not be received)", ip, cfg.DiscoveryUDPPort, err)
				continue
			}
			conns = append(conns, pc.(*net.UDPConn))
			boundIPs = append(boundIPs, ip)
		}
	}
	log.Printf("mol version %s: discovery listening on %s (bound IPs: %v)", version, portStr, boundIPs)
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

	broadcastAddrs := hostinfo.GetPhysicalNICBroadcastAddresses()
	if len(broadcastAddrs) == 0 {
		if cfg.DiscoveryBroadcastAddress != "" {
			broadcastAddrs = []string{cfg.DiscoveryBroadcastAddress}
			log.Printf("discovery: no physical NIC brd found, using config fallback: %v", broadcastAddrs)
		} else {
			broadcastAddrs = []string{"255.255.255.255"}
			log.Printf("discovery: no physical NIC brd found, using 255.255.255.255")
		}
	} else {
		log.Printf("discovery: broadcast addresses (physical NIC brd): %v", broadcastAddrs)
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

	// Web FS: embed embeds "maintenance/web/*" at build time; no separate web/ at runtime.
	fsys, err := fs.Sub(webFS, "maintenance/web")
	if err != nil {
		log.Fatal("web: embedded FS: ", err)
	}
	if _, err := fsys.Open("index.html"); err != nil {
		log.Fatal("web: index.html not in binary (build from repo root with maintenance/web/ present)")
	}
	getHostInfo := func() (hostinfo.Info, error) {
		info, err := hostinfo.Get()
		if err != nil {
			return info, err
		}
		// Use all IPs bound for discovery (so self card shows e.g. 172.29.236.41 and 172.29.237.141)
		for _, b := range boundIPs {
			if b != "0.0.0.0" {
				info.HostIPs = append(info.HostIPs, b)
			}
		}
		if len(info.HostIPs) > 0 {
			info.HostIP = info.HostIPs[0]
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
		InstallPrefix:        cfg.InstallPrefix,
		SSHPort:              cfg.SSHPort,
		SSHUser:              cfg.SSHUser,
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
