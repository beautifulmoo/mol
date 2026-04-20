// Package hostinfocli implements --host-info CLI (GET .../host-info, same as /self when ip empty or self; else UDP unicast Discovery via server).
package hostinfocli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/server"
)

// Run runs: <bin> --host-info -cfg <config> <self|remote-ip>
func Run(args []string) int {
	fs := flag.NewFlagSet("host-info", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("cfg", "", "path to config file (required)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --host-info -cfg <config.yaml> <self|remote-ip>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  GET .../host-info — same as /self when target is self; otherwise UDP unicast Discovery to that IP.\n")
		fmt.Fprintf(os.Stderr, "  Requests always go to the local maintenance HTTP server from config.\n\n")
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
	pos := fs.Args()
	if len(pos) != 1 {
		fmt.Fprintf(os.Stderr, "%s: expected one argument: <self|remote-ip>\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}
	if strings.TrimSpace(*cfgPath) == "" {
		fmt.Fprintf(os.Stderr, "%s: -cfg <config.yaml> is required\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load config: %v\n", appmeta.BinaryName, err)
		return 1
	}

	target := strings.TrimSpace(pos[0])
	if target == "" {
		fmt.Fprintf(os.Stderr, "%s: target must not be empty\n", appmeta.BinaryName)
		return 1
	}

	base := maintenanceHTTPBase(cfg)
	api := normalizeAPIPrefix(cfg.APIPrefix)
	infoURL := base + api + "/host-info"
	var label string
	if strings.EqualFold(target, "self") {
		label = "host self (local)"
	} else {
		if net.ParseIP(target) == nil {
			fmt.Fprintf(os.Stderr, "%s: remote target must be a valid IP address: %q\n", appmeta.BinaryName, target)
			return 1
		}
		label = "host " + target + " (unicast discovery)"
		infoURL += "?ip=" + url.QueryEscape(target)
	}

	// Unicast discovery can take up to ~5s server-side; allow headroom.
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, infoURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: request failed: %v\n", appmeta.BinaryName, err)
		return 1
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read body: %v\n", appmeta.BinaryName, err)
		return 1
	}

	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		fmt.Fprintf(os.Stderr, "%s: parse response: %v\n", appmeta.BinaryName, err)
		return 1
	}
	if envelope.Status != "success" {
		var fail server.APIResponse
		if json.Unmarshal(body, &fail) == nil {
			if s, ok := fail.Data.(string); ok && s != "" {
				fmt.Fprintf(os.Stderr, "%s: %s\n", appmeta.BinaryName, s)
				return 1
			}
		}
		fmt.Fprintf(os.Stderr, "%s: host-info failed: status=%s body=%s\n", appmeta.BinaryName, envelope.Status, strings.TrimSpace(string(body)))
		return 1
	}

	var data discovery.DiscoveryResponse
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		fmt.Fprintf(os.Stderr, "%s: parse host data: %v\n", appmeta.BinaryName, err)
		return 1
	}
	printHostInfo(os.Stdout, label, data)
	return 0
}

func printHostInfo(w io.Writer, label string, d discovery.DiscoveryResponse) {
	fmt.Fprintln(w, label)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(tw, "%s\t%s\n", k, v) }
	row("TYPE", d.Type)
	row("SERVICE", d.Service)
	row("HOSTNAME", d.Hostname)
	row("HOST_IP", d.HostIP)
	if len(d.HostIPs) > 0 {
		row("HOST_IPS", strings.Join(d.HostIPs, ", "))
	}
	row("SERVICE_PORT", strconv.Itoa(d.ServicePort))
	row("VERSION", d.Version)
	if d.RequestID != "" {
		row("REQUEST_ID", d.RequestID)
	}
	row("CPU_INFO", d.CPUInfo)
	row("CPU_USAGE_PERCENT", fmt.Sprintf("%.2f", d.CPUUsagePercent))
	row("CPU_UUID", d.CPUUUID)
	row("MEMORY_TOTAL_MB", strconv.FormatUint(d.MemoryTotalMB, 10))
	row("MEMORY_USED_MB", strconv.FormatUint(d.MemoryUsedMB, 10))
	row("MEMORY_USAGE_PERCENT", fmt.Sprintf("%.2f", d.MemoryUsagePercent))
	if d.RespondedFromIP != "" {
		row("RESPONDED_FROM_IP", d.RespondedFromIP)
	}
	if d.IsSelf {
		row("SELF", "true")
	}
	_ = tw.Flush()
}

func maintenanceHTTPBase(cfg *config.Config) string {
	host := strings.TrimSpace(cfg.MaintenanceListenAddress)
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := cfg.MaintenancePort
	if port <= 0 {
		port = 8889
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func normalizeAPIPrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/api/v1"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}
