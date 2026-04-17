package maintenance

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
	"strings"
	"syscall"
	"time"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/discoverycli"
	"contrabass-agent/maintenance/hostinfo"
	"contrabass-agent/maintenance/server"
)

const helpText = `Contrabass agent — Discovery 및 웹 UI

사용법:
  <bin> -cfg <파일>     설정 파일을 지정해야 HTTP 서버 + Discovery가 시작됩니다 (필수)
  <bin>                  인자 없이 실행 시 버전 안내 후 종료 (서비스는 시작하지 않음)

옵션:
  -h, --help             이 도움말 출력
  -version, --version    버전 출력 후 종료
  --nic-brd              Discovery와 동일 규칙으로 인터페이스별 IPv4 brd 출력 (확인용) 후 종료
  --discovery [flags]    설정 없이 UDP Discovery만 수행 (<bin> --discovery -h)

`

//go:embed web/*
var webFS embed.FS

func versionLine(buildVersionKey string) string {
	v := strings.TrimSpace(buildVersionKey)
	if v == "" {
		v = "0.0.0-0"
	}
	return appmeta.BinaryName + " " + v
}

func printMustSpecifyConfig(binVersion string) {
	fmt.Println(versionLine(binVersion))
	fmt.Println()
	fmt.Println("HTTP 서비스와 Discovery를 시작하려면 설정 파일을 지정하세요.")
	fmt.Printf("  %s -cfg <config.yaml>\n", appmeta.BinaryName)
	fmt.Println()
	fmt.Println("자세한 옵션은 다음을 실행하세요.")
	fmt.Printf("  %s -h\n", appmeta.BinaryName)
	fmt.Printf("  %s --help\n", appmeta.BinaryName)
}

// setSOReuseport sets SO_REUSEPORT on a socket (Linux only).
func setSOReuseport(fd int) error {
	const soReuseport = 15 // SO_REUSEPORT on Linux (amd64/arm64; not in syscall package as named const)
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, soReuseport, 1)
}

// ShouldStartGinReverseProxy returns true only for `program -cfg <path>` with a non-empty path.
// That is the long-running service mode where main also starts the Gin reverse proxy (Server.HTTPPort).
// Any other invocation does not start Gin: --discovery, --nic-brd, -h/--help, --version, no args, or unknown first arg
// (because args[1] must be exactly "-cfg").
func ShouldStartGinReverseProxy(args []string) bool {
	if len(args) < 3 {
		return false
	}
	if args[1] != "-cfg" {
		return false
	}
	return strings.TrimSpace(args[2]) != ""
}

// Run starts the maintenance HTTP server and Discovery. buildVersionKey is the full key from main (ldflags -X main.VersionKey=…, see Makefile / scripts/build-version.sh).
// args is typically os.Args; returns 0 for success and 1 for failure (for main to os.Exit). Does not call os.Exit.
func Run(buildVersionKey string, args []string) int {
	if len(args) <= 1 {
		printMustSpecifyConfig(buildVersionKey)
		return 0
	}
	if len(args) >= 2 {
		switch args[1] {
		case "-h", "--help":
			fmt.Print(strings.ReplaceAll(helpText, "<bin>", appmeta.BinaryName))
			return 0
		case "--version", "-version":
			fmt.Println(versionLine(buildVersionKey))
			return 0
		case "--nic-brd":
			pairs := hostinfo.GetPhysicalNICBrdPairs()
			for _, p := range pairs {
				fmt.Printf("%s : %s\n", p.Iface, p.Brd)
			}
			return 0
		case "--discovery":
			return discoverycli.Run(args[2:])
		}
	}
	if args[1] != "-cfg" {
		fmt.Fprintf(os.Stderr, "알 수 없는 인자: %q\n\n", args[1])
		printMustSpecifyConfig(buildVersionKey)
		return 1
	}
	if len(args) < 3 {
		fmt.Fprintf(os.Stderr, "%s: -cfg 다음에 설정 파일 경로가 필요합니다.\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "예: %s -cfg /var/lib/contrabass/mole/config.yaml\n", appmeta.BinaryName)
		return 1
	}
	cfgPath := args[2]
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}
	if cfg.MaintenancePort <= 0 || cfg.MaintenancePort > 65535 {
		log.Printf("config: MaintenancePort must be 1..65535 (got %d)", cfg.MaintenancePort)
		return 1
	}
	if cfg.ServerHTTPPort <= 0 || cfg.ServerHTTPPort > 65535 {
		log.Printf("config: Server.HTTPPort must be 1..65535 (got %d)", cfg.ServerHTTPPort)
		return 1
	}
	listenHost := strings.TrimSpace(cfg.MaintenanceListenAddress)
	if listenHost == "" {
		log.Printf("config: MaintenanceListenAddress is required (e.g. 127.0.0.1 or 0.0.0.0)")
		return 1
	}
	displayVersion := strings.TrimSpace(buildVersionKey)
	if displayVersion == "" {
		displayVersion = "0.0.0-0"
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
		log.Printf("listen discovery: %v", err)
		return 1
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
	log.Printf("%s version %s: discovery listening on %s (bound IPs: %v)", appmeta.BinaryName, displayVersion, portStr, boundIPs)
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
			log.Printf("discovery: no brd addresses collected (3.1.1), using config fallback: %v", broadcastAddrs)
		} else {
			broadcastAddrs = []string{"255.255.255.255"}
			log.Printf("discovery: no brd addresses collected (3.1.1), using 255.255.255.255")
		}
	} else {
		log.Printf("discovery: broadcast addresses: %v", broadcastAddrs)
	}
	discCfg := discovery.Config{
		DiscoveryServiceName:        cfg.DiscoveryServiceName,
		DiscoveryBroadcastAddresses: broadcastAddrs,
		DiscoveryUDPPort:            cfg.DiscoveryUDPPort,
		DiscoveryTimeoutSeconds:       cfg.DiscoveryTimeoutSeconds,
		DiscoveryDeduplicate:        cfg.DiscoveryDeduplicate,
		Version:                     displayVersion,
		ServicePort:                 cfg.MaintenancePort,
	}
	disc := discovery.New(discCfg, conns, getter)
	go disc.Run()

	// Web FS: embed embeds "web/*" under this package at build time; no separate web/ at runtime.
	fsys, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Printf("web: embedded FS: %v", err)
		return 1
	}
	if _, err := fsys.Open("index.html"); err != nil {
		log.Print("web: index.html not in binary (build from repo root with maintenance/web/ present)")
		return 1
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
		Version:              displayVersion,
		ServicePort:          cfg.MaintenancePort,
		RemoteProxyPort:      cfg.ServerHTTPPort,
		DiscoveryServiceName: cfg.DiscoveryServiceName,
		SystemctlServiceName: cfg.SystemctlServiceName,
		DeployBase:           cfg.DeployBase,
		InstallPrefix:        cfg.InstallPrefix,
		SSHPort:              cfg.SSHPort,
		SSHUser:              cfg.SSHUser,
		MaxUploadBytes:       cfg.MaxUploadBytes.Int(),
		RemoteHealthCheckIntervalSeconds:  cfg.RemoteHealth.IntervalSeconds,
		RemoteHealthCheckTimeoutSeconds:   cfg.RemoteHealth.TimeoutSeconds,
		RemoteHealthCheckFailureThreshold: cfg.RemoteHealth.FailureThreshold,
		RemoteHealthCheckJitterSeconds:    cfg.RemoteHealth.JitterSeconds,
	})

	// maintenance HTTP is typically internal-only; access via Gin(8888) reverse proxy.
	listenAddr := net.JoinHostPort(listenHost, strconv.Itoa(cfg.MaintenancePort))
	httpSrv := &http.Server{Addr: listenAddr, Handler: srv.Handler()}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Printf("http listen: %v", err)
		return 1
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	log.Printf("%s stopped", appmeta.BinaryName)
	return 0
}
