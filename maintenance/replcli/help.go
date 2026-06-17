package replcli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"contrabass-agent/maintenance/clirest"
	"contrabass-agent/maintenance/discoverycli"
)

const prompt = "contrabass-agent> "

func printHelp(topic string) {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic != "" {
		if line, ok := helpTopics[topic]; ok {
			fmt.Println(line)
			return
		}
		fmt.Fprintf(os.Stderr, "unknown help topic: %q (try 'help' for full list)\n", topic)
		return
	}
	fmt.Print(helpText)
}

const helpText = `Contrabass agent interactive REPL (agent CLI only; does not start -cfg service).

Prompt: contrabass-agent>

Session
  set <key> <value>     Session defaults (persist until exit)
  show                  Print session settings and cached host count
  hosts                 List remotes from last 'discover' (bulk targets)
  clear-hosts           Clear discovery cache

Meta
  help [command]        This help or command-specific help
  version               Print agent version line
  exit, quit            Leave REPL

Discovery (UDP, no orchestrator service required)
  discover [flags]      Run discovery and cache remotes for bulk commands
                        Flags: --dest-port=N --src-port=N --timeout=N --service=name
  nic-brd               Print NIC broadcast addresses (same rules as CLI)

Single-host (target Gin HTTP, default port 8888)
  host-info <self|local|ip>   Uses last 'discover' cache for HOST_IP/HOST_IPS when available
  versions-list <self|local|ip>
  versions-switch <self|local|ip> <version-key>
  apply-update <self|local|ip> <bundle.tar.gz>
                        Honors session agent-variant and use-bundle-config unless
                        overridden with flags on the command line

Bulk remotes (local maintenance HTTP, default port 8889; orchestrator -cfg must be running)
  push-config-all       Push local current config to cached hosts
  restart-all           Restart cached hosts
  apply-update-all <bundle.tar.gz>
                        Upload bundle to local staging, then apply to cached hosts
  rollback-all          Rollback cached hosts to previous version

Session keys for 'set'
  apiprefix <path>              Default /maintenance/api/v1
  maintenance-port <N>          Default 8889 (bulk commands)
  agent-variant control|compute For apply-update / apply-update-all (optional)
  use-bundle-config on|off      Default off (reuse each remote's current config)

Notes
  - Bulk commands use hosts from the last successful 'discover', not rediscovery.
  - Run 'discover' before push-config-all, restart-all, apply-update-all, rollback-all.
  - Single-host commands use -apiprefix from session when set.
  - Command-line flags on apply-update / discover override session for that line only.
  - Tab completes commands, help topics, set keys, discover flags, targets (self/local/cached IPs), bundle paths.

`

var helpTopics = map[string]string{
	"discover": `discover [--dest-port=N] [--src-port=N] [--timeout=N] [--service=name]
  UDP discovery (defaults: dest 9999, src 9998, timeout 10s, service Mole-Discovery).
  Results are printed and cached for bulk commands.`,
	"hosts": `hosts
  Show primary_ip, hostname, cpu_uuid, and ips[] from the last discover.`,
	"set": `set <key> <value>
  Keys: apiprefix, maintenance-port, agent-variant, use-bundle-config (on|off|true|false)`,
	"push-config-all": `push-config-all
  POST .../current-config/push-local-all for cached hosts.`,
	"restart-all": `restart-all
  POST .../service-control/restart-all for cached hosts.`,
	"apply-update-all": `apply-update-all <bundle.tar.gz>
  Upload to local staging, then POST .../apply-update-all for cached hosts.`,
	"rollback-all": `rollback-all
  POST .../versions/rollback-all for cached hosts.`,
}

func printSession(s *Session) {
	fmt.Println("Session settings:")
	fmt.Printf("  apiprefix:         %s\n", s.effectiveAPIPrefix())
	fmt.Printf("  maintenance-port:  %d\n", s.MaintenancePort)
	if v := strings.TrimSpace(s.AgentVariant); v != "" {
		fmt.Printf("  agent-variant:     %s\n", v)
	} else {
		fmt.Printf("  agent-variant:     (not set; apply uses CLI build_variant or compute)\n")
	}
	fmt.Printf("  use-bundle-config: %v\n", s.UseBundleConfig)
	fmt.Printf("  cached hosts:      %d\n", len(s.CachedHosts))
	if len(s.LastDiscovery) > 0 {
		fmt.Printf("  last discovery:    %d response(s)\n", len(s.LastDiscovery))
	}
}

func printHosts(s *Session) {
	if len(s.CachedHosts) == 0 {
		fmt.Println("No cached remote hosts. Run 'discover' first.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PRIMARY_IP\tHOSTNAME\tCPU_UUID\tIPS")
	for _, h := range s.CachedHosts {
		ips := strings.Join(h.IPs, ",")
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", h.PrimaryIP, h.Hostname, h.CPUUUID, ips)
	}
	_ = w.Flush()
}

func runSet(s *Session, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: set <key> <value>")
	}
	key := strings.ToLower(strings.TrimSpace(args[0]))
	val := strings.TrimSpace(args[1])
	switch key {
	case "apiprefix":
		s.APIPrefix = val
	case "maintenance-port", "maintenance_port":
		n, err := strconv.Atoi(val)
		if err != nil || n <= 0 || n > 65535 {
			return fmt.Errorf("maintenance-port must be 1..65535")
		}
		s.MaintenancePort = n
	case "agent-variant", "agent_variant":
		v, err := clirest.ResolveAgentVariantForTarget(val, "")
		if err != nil {
			return err
		}
		s.AgentVariant = v
	case "use-bundle-config", "use_bundle_config":
		switch strings.ToLower(val) {
		case "on", "true", "1", "yes":
			s.UseBundleConfig = true
		case "off", "false", "0", "no":
			s.UseBundleConfig = false
		default:
			return fmt.Errorf("use-bundle-config: use on or off")
		}
	default:
		return fmt.Errorf("unknown session key %q (try: apiprefix, maintenance-port, agent-variant, use-bundle-config)", key)
	}
	return nil
}

func runDiscover(s *Session, args []string) error {
	opts, err := discoverycli.ParseDiscoverArgs(args)
	if err != nil {
		return err
	}
	list, err := discoverycli.DiscoverToStdout(opts)
	if err != nil {
		return err
	}
	for _, line := range discoverycli.FormatDiscoveryResultLines(list) {
		fmt.Println(line)
	}
	s.setCachedDiscovery(list)
	fmt.Printf("Cached %d remote host(s) for bulk commands.\n", len(s.CachedHosts))
	return nil
}
