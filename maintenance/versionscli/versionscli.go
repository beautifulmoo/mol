// Package versionscli implements --versions-list and --versions-switch CLIs (GET versions/list, POST versions/switch-current).
package versionscli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/cliutil"
	"contrabass-agent/maintenance/server"
	"contrabass-agent/maintenance/versionsapi"
)

// RunList runs: <bin> --versions-list -cfg <config> <self|remote-ip>
func RunList(args []string) int {
	cfgPath, pos, showHelp, err := parseVersionsListArgs(args)
	if showHelp {
		printVersionsListUsage()
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		printVersionsListUsage()
		return 1
	}
	if len(pos) != 1 {
		fmt.Fprintf(os.Stderr, "%s: expected exactly one argument: <self|remote-ip>\n", appmeta.BinaryName)
		printVersionsListUsage()
		return 1
	}
	if strings.TrimSpace(cfgPath) == "" {
		fmt.Fprintf(os.Stderr, "%s: -cfg <config.yaml> is required\n", appmeta.BinaryName)
		printVersionsListUsage()
		return 1
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load config: %v\n", appmeta.BinaryName, err)
		return 1
	}

	target := strings.TrimSpace(pos[0])
	if target == "" {
		fmt.Fprintf(os.Stderr, "%s: target must not be empty\n", appmeta.BinaryName)
		return 1
	}

	if strings.EqualFold(target, "self") {
		base := versionsapi.VersionsBaseFromConfig(cfg)
		rows, err := versionsapi.ListInstalledVersions(base)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: list versions: %v\n", appmeta.BinaryName, err)
			return 1
		}
		printVersionsTable(os.Stdout, "", rows)
		return 0
	}

	if net.ParseIP(target) == nil {
		fmt.Fprintf(os.Stderr, "%s: remote target must be a valid IP address: %q\n", appmeta.BinaryName, target)
		return 1
	}
	remoteIP := target
	dialAddr := cliutil.RemoteDialAddr(cfg, target)
	if err := cliutil.DialTCP(dialAddr, 5*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot connect to %s: %v\n", appmeta.BinaryName, dialAddr, err)
		return 1
	}
	listURL := cliutil.RemoteBaseURL(cfg, target) + cliutil.NormalizeAPIPrefix(cfg.APIPrefix) + "/versions/list"

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, listURL, nil)
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
		fmt.Fprintf(os.Stderr, "%s: list failed: status=%s body=%s\n", appmeta.BinaryName, envelope.Status, strings.TrimSpace(string(body)))
		return 1
	}

	var payload struct {
		Versions []versionsapi.VersionEntry `json:"versions"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "%s: parse versions: %v\n", appmeta.BinaryName, err)
		return 1
	}
	printVersionsTable(os.Stdout, remoteIP, payload.Versions)
	return 0
}

func printVersionsListUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s --versions-list -cfg <config.yaml> <self|remote-ip>\n\n", appmeta.BinaryName)
	fmt.Fprintf(os.Stderr, "  self: list from local disk (DeployBase/InstallPrefix; no HTTP).\n")
	fmt.Fprintf(os.Stderr, "  remote IP: GET http://<ip>:Server.HTTPPort{APIPrefix}/versions/list on that host (Gin; no local agent required).\n\n")
}

func parseVersionsListArgs(args []string) (cfgPath string, pos []string, showHelp bool, err error) {
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			showHelp = true
			i++
		case a == "-cfg" || a == "--cfg":
			if i+1 >= len(args) {
				return "", nil, false, fmt.Errorf("-cfg requires a path argument")
			}
			cfgPath = args[i+1]
			i += 2
		case strings.HasPrefix(a, "-cfg="):
			cfgPath = strings.TrimSpace(strings.TrimPrefix(a, "-cfg="))
			i++
		case strings.HasPrefix(a, "--cfg="):
			cfgPath = strings.TrimSpace(strings.TrimPrefix(a, "--cfg="))
			i++
		case strings.HasPrefix(a, "-"):
			return "", nil, false, fmt.Errorf("unknown flag %q", a)
		default:
			pos = append(pos, a)
			i++
		}
	}
	return cfgPath, pos, showHelp, nil
}

func printVersionsTable(w io.Writer, remoteIP string, rows []versionsapi.VersionEntry) {
	if remoteIP != "" {
		fmt.Fprintf(w, "host %s\n", remoteIP)
	} else {
		fmt.Fprintln(w, "host self (local)")
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no versions)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tCURRENT\tPREVIOUS")
	for _, r := range rows {
		cur, prev := "no", "no"
		if r.IsCurrent {
			cur = "yes"
		}
		if r.IsPrevious {
			prev = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Version, cur, prev)
	}
	_ = tw.Flush()
}

// RunSwitch runs: <bin> --versions-switch -cfg <config> <self|remote-ip> <version-key>
func RunSwitch(args []string) int {
	fs := flag.NewFlagSet("versions-switch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("cfg", "", "path to config file (required)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --versions-switch -cfg <config.yaml> <self|remote-ip> <version-key>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  POST .../versions/switch-current — run embedded update.sh via systemd-run (same as web).\n")
		fmt.Fprintf(os.Stderr, "  self: run embedded update.sh via systemd-run (same as API); no local HTTP service required.\n")
		fmt.Fprintf(os.Stderr, "  remote IP: POST to that host's Gin (Server.HTTPPort); no local agent required.\n")
		fmt.Fprintf(os.Stderr, "  The version must already exist under versions/ (or staging) on the target host.\n\n")
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
	if len(pos) != 2 {
		fmt.Fprintf(os.Stderr, "%s: expected two arguments: <self|remote-ip> <version-key>\n", appmeta.BinaryName)
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
	version := strings.TrimSpace(pos[1])
	if target == "" || version == "" {
		fmt.Fprintf(os.Stderr, "%s: target and version must not be empty\n", appmeta.BinaryName)
		return 1
	}
	if err := config.ValidateVersionKeyPath(version); err != nil {
		fmt.Fprintf(os.Stderr, "%s: invalid version key: %v\n", appmeta.BinaryName, err)
		return 1
	}

	if strings.EqualFold(target, "self") {
		if err := versionsapi.RunSwitchCurrentWithRoots(
			versionsapi.DeployRootFromConfig(cfg),
			cfg.InstallPrefix,
			cfg.DeployBase,
			version,
		); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
			return 1
		}
		fmt.Println("systemd-run started update.sh. Restart may take tens of seconds; check update_history.log or journal on failure.")
		return 0
	}

	if net.ParseIP(target) == nil {
		fmt.Fprintf(os.Stderr, "%s: remote target must be a valid IP address: %q\n", appmeta.BinaryName, target)
		return 1
	}
	addr := cliutil.RemoteDialAddr(cfg, target)
	if err := cliutil.DialTCP(addr, 5*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot connect to %s: %v\n", appmeta.BinaryName, addr, err)
		return 1
	}
	api := cliutil.NormalizeAPIPrefix(cfg.APIPrefix)
	switchURL := cliutil.RemoteBaseURL(cfg, target) + api + "/versions/switch-current"
	body := map[string]string{"version": version}

	payload, err := json.Marshal(body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}

	client := &http.Client{Timeout: 300 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, switchURL, bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
		return 1
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: request failed: %v\n", appmeta.BinaryName, err)
		return 1
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read body: %v\n", appmeta.BinaryName, err)
		return 1
	}

	var out server.APIResponse
	if json.Unmarshal(respBody, &out) != nil {
		fmt.Fprintf(os.Stderr, "%s: parse response: %s\n", appmeta.BinaryName, strings.TrimSpace(string(respBody)))
		return 1
	}
	if out.Status != "success" {
		if s, ok := out.Data.(string); ok && s != "" {
			fmt.Fprintf(os.Stderr, "%s: %s\n", appmeta.BinaryName, s)
		} else {
			fmt.Fprintf(os.Stderr, "%s: switch failed: status=%s\n", appmeta.BinaryName, out.Status)
		}
		return 1
	}
	if msg, ok := out.Data.(string); ok && msg != "" {
		fmt.Println(msg)
	} else {
		fmt.Println("Switch-current requested successfully.")
	}
	return 0
}
