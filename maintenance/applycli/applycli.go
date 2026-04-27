// Package applycli implements `contrabass-moleU agent --apply-update` (bundle preflight + upload + apply in one shot).
package applycli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"contrabass-agent/maintenance/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/cliutil"
	"contrabass-agent/maintenance/server"
	"contrabass-agent/maintenance/versionsapi"
)

const bundleFormField = "bundle" // same as server.uploadBundleField

// Run parses flags and runs apply-update CLI. buildVersionKey is the running binary's version (ldflags), used for local policy like GET /self.
//
//	<bin> agent --apply-update -cfg <config.yaml> <self|remote-ip> <bundle.tar.gz>
func Run(buildVersionKey string, args []string) int {
	fs := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("cfg", "", "path to config file (required)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s agent --apply-update -cfg <config.yaml> <self|remote-ip> <bundle.tar.gz>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Validates the bundle, compares versions, and uploads/applies only when an update is allowed.\n")
		fmt.Fprintf(os.Stderr, "  self: stage bundle and apply locally (no local maintenance HTTP; typically sudo for /var/lib/... and systemd-run).\n")
		fmt.Fprintf(os.Stderr, "  remote-ip: multipart POST to that host's Gin (Server.HTTPPort); no local agent required.\n\n")
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
		fmt.Fprintf(os.Stderr, "%s: expected two arguments: <self|remote-ip> <bundle.tar.gz>\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}
	if strings.TrimSpace(*cfgPath) == "" {
		fmt.Fprintf(os.Stderr, "%s: -cfg <config.yaml> is required\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}

	target := strings.TrimSpace(pos[0])
	bundlePath := strings.TrimSpace(pos[1])
	if target == "" || bundlePath == "" {
		fmt.Fprintf(os.Stderr, "%s: target and bundle path must not be empty\n", appmeta.BinaryName)
		return 1
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load config: %v\n", appmeta.BinaryName, err)
		return 1
	}

	maxBytes := config.ClampMaxUploadBytes(cfg.MaxUploadBytes.Int())

	fi, err := os.Stat(bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: bundle: %v\n", appmeta.BinaryName, err)
		return 1
	}
	if fi.Size() > maxBytes {
		fmt.Fprintf(os.Stderr, "%s: bundle size %d exceeds configured limit %d\n", appmeta.BinaryName, fi.Size(), maxBytes)
		return 1
	}
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: read bundle: %v\n", appmeta.BinaryName, err)
		return 1
	}

	versionKey, configData, _, workDir, agentSrc, err := server.PrepareAgentBundleFromReader(os.TempDir(), bytes.NewReader(raw), maxBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: bundle validation failed: %v\n", appmeta.BinaryName, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	httpClient := &http.Client{Timeout: 300 * time.Second}
	apiPrefix := cliutil.NormalizeAPIPrefix(cfg.APIPrefix)

	switch strings.ToLower(target) {
	case "self":
		cur := currentVersionKeyForApply(buildVersionKey, cfg)
		if !config.StagingUpdateAvailable(versionKey, cur) {
			fmt.Fprintf(os.Stderr, "%s: update not needed or not allowed by policy (bundle %q, current %q)\n", appmeta.BinaryName, versionKey, cur)
			return 1
		}
		fmt.Printf("Applying bundle %s locally (current %s)\n", versionKey, cur)
		if err := server.ApplyUpdateSelfFromBundleExtract(cfg, raw, versionKey, configData, agentSrc); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", appmeta.BinaryName, err)
			return 1
		}
		fmt.Println("Apply update requested; the agent will restart shortly.")
		return 0

	default:
		remoteIP := target
		if net.ParseIP(remoteIP) == nil {
			fmt.Fprintf(os.Stderr, "%s: remote target must be a valid IP address: %q\n", appmeta.BinaryName, remoteIP)
			return 1
		}
		addr := cliutil.RemoteDialAddr(cfg, remoteIP)
		if err := cliutil.DialTCP(addr, 5*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "%s: cannot connect to %s (agent HTTP port must be reachable): %v\n", appmeta.BinaryName, addr, err)
			return 1
		}
		remoteBase := cliutil.RemoteBaseURL(cfg, remoteIP)
		cur, err := fetchVersionGET(httpClient, remoteBase+apiPrefix+"/self")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: get remote version failed (%s): %v\n", appmeta.BinaryName, remoteBase+apiPrefix+"/self", err)
			return 1
		}
		if !config.StagingUpdateAvailable(versionKey, cur) {
			fmt.Fprintf(os.Stderr, "%s: update not needed or not allowed by policy (bundle %q, remote current %q)\n", appmeta.BinaryName, versionKey, cur)
			return 1
		}
		fmt.Printf("Applying bundle %s to remote %s (remote current %s)\n", versionKey, remoteIP, cur)
		applyURL := remoteBase + apiPrefix + "/apply-update"
		if err := postMultipartApplyRemote(httpClient, applyURL, remoteIP, bundlePath); err != nil {
			fmt.Fprintf(os.Stderr, "%s: remote apply failed: %v\n", appmeta.BinaryName, err)
			return 1
		}
		fmt.Printf("Remote %s updated to version %s.\n", remoteIP, versionKey)
		return 0
	}
}

// currentVersionKeyForApply is the "installed current" for policy: DeployBase/current → versions/<name>,
// not the CLI binary's ldflags (the binary may be a dev build while /var/lib/... still points at an older key).
func currentVersionKeyForApply(buildVersionKey string, cfg *config.Config) string {
	deploy := versionsapi.DeployRootFromConfig(cfg)
	if cur := versionsapi.ResolveSymlinkVersion(deploy, "current"); cur != "" {
		return cur
	}
	return strings.TrimSpace(buildVersionKey)
}

func fetchVersionGET(client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("JSON: %w", err)
	}
	if out.Status != "success" {
		return "", fmt.Errorf("status %q", out.Status)
	}
	return strings.TrimSpace(out.Data.Version), nil
}

func postMultipartApplyRemote(client *http.Client, applyURL, remoteIP, bundlePath string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("ip", remoteIP); err != nil {
		return err
	}
	part, err := w.CreateFormFile(bundleFormField, filepath.Base(bundlePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, applyURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out server.APIResponse
	if json.Unmarshal(body, &out) != nil {
		return fmt.Errorf("parse remote apply response: %s", strings.TrimSpace(string(body)))
	}
	if out.Status != "success" {
		if s, ok := out.Data.(string); ok && s != "" {
			return fmt.Errorf("%s", s)
		}
		return fmt.Errorf("remote apply failed: status=%s", out.Status)
	}
	return nil
}
