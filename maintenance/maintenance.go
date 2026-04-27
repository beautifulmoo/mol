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
	"contrabass-agent/maintenance/config"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/applycli"
	"contrabass-agent/maintenance/discoverycli"
	"contrabass-agent/maintenance/hostinfocli"
	"contrabass-agent/maintenance/hostinfo"
	"contrabass-agent/maintenance/server"
	"contrabass-agent/maintenance/versionscli"
)

const helpText = `Contrabass agent — Discovery and maintenance web UI

Usage:
  <bin> -cfg <file>         Start HTTP server + Discovery (config file required)
  <bin>                     Print version hint and exit (no service)

Other commands require the "agent" subcommand first (not used for HTTP service).

Options (after "agent"):
  -h, --help               Show this help
  -version, --version      Print version and exit
  --host-info [flags]      Host info (local /self or unicast discovery) (<bin> agent --host-info -h)
  --nic-brd                Print per-interface IPv4 broadcast addresses (same rules as Discovery), then exit
  --discovery [flags]      Run UDP Discovery only, no config (<bin> agent --discovery -h)
  --apply-update [flags]   Validate bundle and apply locally or to remote Gin (<bin> agent --apply-update -h)
  --versions-list [flags]  List installed versions (local or remote) (<bin> agent --versions-list -h)
  --versions-switch [flags] Switch current version (<bin> agent --versions-switch -h)

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
	fmt.Println("To start HTTP service and Discovery, pass a config file:")
	fmt.Printf("  %s -cfg <config.yaml>\n", appmeta.BinaryName)
	fmt.Println()
	fmt.Println("For discovery, host-info, and other CLI commands:")
	fmt.Printf("  %s agent --help\n", appmeta.BinaryName)
}

// setSOReuseport sets SO_REUSEPORT on a socket (Linux only).
func setSOReuseport(fd int) error {
	const soReuseport = 15 // SO_REUSEPORT on Linux (amd64/arm64; not in syscall package as named const)
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, soReuseport, 1)
}

// ConfigPathForServiceMode returns the config file path for long-running HTTP+Discovery service, or "".
// Accepted forms: `program -cfg <path>` (preferred) or legacy `program agent -cfg <path>`.
func ConfigPathForServiceMode(args []string) string {
	if len(args) >= 3 && args[1] == "-cfg" {
		if p := strings.TrimSpace(args[2]); p != "" {
			return p
		}
	}
	if len(args) >= 4 && strings.EqualFold(args[1], "agent") && args[2] == "-cfg" {
		return strings.TrimSpace(args[3])
	}
	return ""
}

// ShouldStartGinReverseProxy is true when main should start the Gin reverse proxy (Server.HTTPPort).
func ShouldStartGinReverseProxy(args []string) bool {
	return ConfigPathForServiceMode(args) != ""
}

// runServiceWithConfigPath starts maintenance HTTP, UDP discovery, and embedded web UI.
func runServiceWithConfigPath(buildVersionKey, cfgPath string) int {
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

// Run starts the maintenance HTTP server and Discovery. buildVersionKey is the full key from main (ldflags -X main.VersionKey=…, see Makefile / maintenance/scripts/build-version.sh).
// args is typically os.Args; returns 0 for success and 1 for failure (for main to os.Exit). Does not call os.Exit.
func Run(buildVersionKey string, args []string) int {
	if len(args) <= 1 {
		printMustSpecifyConfig(buildVersionKey)
		return 0
	}

	if args[1] == "-cfg" {
		if len(args) < 3 || strings.TrimSpace(args[2]) == "" {
			fmt.Fprintf(os.Stderr, "%s: path to config file required after -cfg\n", appmeta.BinaryName)
			fmt.Fprintf(os.Stderr, "example: %s -cfg /var/lib/contrabass/mole/config.yaml\n", appmeta.BinaryName)
			return 1
		}
		return runServiceWithConfigPath(buildVersionKey, args[2])
	}

	// Transitional: root --version for older update flows; prefer agent --version long-term.
	if args[1] == "--version" || args[1] == "-version" {
		fmt.Println(versionLine(buildVersionKey))
		return 0
	}

	if !strings.EqualFold(strings.TrimSpace(args[1]), "agent") {
		fmt.Fprintf(os.Stderr, "%s: start HTTP+Discovery with %s -cfg <config.yaml>, or use %q for other commands (e.g. %s agent --help)\n", appmeta.BinaryName, appmeta.BinaryName, "agent", appmeta.BinaryName)
		printMustSpecifyConfig(buildVersionKey)
		return 1
	}
	if len(args) == 2 {
		fmt.Fprintf(os.Stderr, "%s: %q requires a command after it (e.g. %s agent --help)\n", appmeta.BinaryName, "agent", appmeta.BinaryName)
		return 1
	}
	args = append([]string{args[0]}, args[2:]...)

	if args[1] == "-cfg" {
		if len(args) < 3 || strings.TrimSpace(args[2]) == "" {
			fmt.Fprintf(os.Stderr, "%s: path to config file required after -cfg\n", appmeta.BinaryName)
			fmt.Fprintf(os.Stderr, "example: %s -cfg /var/lib/contrabass/mole/config.yaml\n", appmeta.BinaryName)
			return 1
		}
		return runServiceWithConfigPath(buildVersionKey, args[2])
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
		case "--apply-update":
			return applycli.Run(buildVersionKey, args[2:])
		case "--versions-list":
			return versionscli.RunList(args[2:])
		case "--versions-switch":
			return versionscli.RunSwitch(args[2:])
		case "--host-info":
			return hostinfocli.Run(buildVersionKey, args[2:])
		}
	}
	fmt.Fprintf(os.Stderr, "unknown argument: %q\n\n", args[1])
	printMustSpecifyConfig(buildVersionKey)
	return 1
}
