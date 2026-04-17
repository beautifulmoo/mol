// Package applycli implements `contrabass-moleU --apply-update` (bundle preflight + upload + apply in one shot).
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
	"strconv"
	"strings"
	"time"

	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/server"
)

const bundleFormField = "bundle" // same as server.uploadBundleField

// Run parses flags and runs apply-update CLI. Expected invocation:
//
//	<bin> --apply-update -cfg <config.yaml> <self|remote-ip> <bundle.tar.gz>
func Run(args []string) int {
	fs := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("cfg", "", "path to config file (required)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --apply-update -cfg <config.yaml> <self|remote-ip> <bundle.tar.gz>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  Validates the bundle, compares versions, and uploads/applies only when an update is allowed.\n")
		fmt.Fprintf(os.Stderr, "  self: upload via local maintenance API, then apply locally.\n")
		fmt.Fprintf(os.Stderr, "  remote-ip: multipart apply-update on local maintenance API; bundle is sent to the remote host.\n\n")
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

	maxBytes := normalizeMaxUploadBytes(cfg.MaxUploadBytes.Int())

	f, err := os.Open(bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: open bundle: %v\n", appmeta.BinaryName, err)
		return 1
	}
	bundleReader := io.Reader(f)
	if fi, err := f.Stat(); err == nil && fi.Size() > maxBytes {
		_ = f.Close()
		fmt.Fprintf(os.Stderr, "%s: bundle size %d exceeds configured limit %d\n", appmeta.BinaryName, fi.Size(), maxBytes)
		return 1
	}

	versionKey, _, _, workDir, _, err := server.PrepareAgentBundleFromReader(os.TempDir(), bundleReader, maxBytes)
	_ = f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: bundle validation failed: %v\n", appmeta.BinaryName, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	maintenanceBase := maintenanceHTTPBase(cfg)
	apiPrefix := normalizeAPIPrefix(cfg.APIPrefix)

	httpClient := &http.Client{Timeout: 300 * time.Second}

	switch strings.ToLower(target) {
	case "self":
		cur, err := fetchVersionGET(httpClient, maintenanceBase+apiPrefix+"/self")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: get local version failed (%s): %v\n", appmeta.BinaryName, maintenanceBase+apiPrefix+"/self", err)
			return 1
		}
		if !config.StagingUpdateAvailable(versionKey, cur) {
			fmt.Fprintf(os.Stderr, "%s: update not needed or not allowed by policy (bundle %q, current %q)\n", appmeta.BinaryName, versionKey, cur)
			return 1
		}
		fmt.Printf("Applying bundle %s locally (current %s)\n", versionKey, cur)
		if err := postUploadBundle(httpClient, maintenanceBase+apiPrefix+"/upload", bundlePath); err != nil {
			fmt.Fprintf(os.Stderr, "%s: upload failed: %v\n", appmeta.BinaryName, err)
			return 1
		}
		if err := postApplyUpdateJSON(httpClient, maintenanceBase+apiPrefix+"/apply-update", versionKey); err != nil {
			fmt.Fprintf(os.Stderr, "%s: apply request failed: %v\n", appmeta.BinaryName, err)
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
		httpPort := cfg.ServerHTTPPort
		if httpPort <= 0 {
			httpPort = 8888
		}
		addr := net.JoinHostPort(remoteIP, strconv.Itoa(httpPort))
		if err := dialTCP(addr, 5*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "%s: cannot connect to %s (agent HTTP port must be reachable): %v\n", appmeta.BinaryName, addr, err)
			return 1
		}
		remoteBase := fmt.Sprintf("http://%s", addr)
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
		applyURL := maintenanceBase + apiPrefix + "/apply-update"
		if err := postMultipartApplyRemote(httpClient, applyURL, remoteIP, bundlePath); err != nil {
			fmt.Fprintf(os.Stderr, "%s: remote apply failed: %v\n", appmeta.BinaryName, err)
			return 1
		}
		fmt.Printf("Remote %s updated to version %s.\n", remoteIP, versionKey)
		return 0
	}
}

func normalizeMaxUploadBytes(n int) int64 {
	const minB = int64(1 << 20)
	const maxB = int64(10 << 30)
	if n <= 0 {
		return int64(config.DefaultMaxUploadBytes)
	}
	v := int64(n)
	if v < minB {
		return minB
	}
	if v > maxB {
		return maxB
	}
	return v
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

func dialTCP(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
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

func postUploadBundle(client *http.Client, uploadURL, bundlePath string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, uploadURL, &buf)
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
		return fmt.Errorf("parse upload response: %s", strings.TrimSpace(string(body)))
	}
	if out.Status != "success" {
		if s, ok := out.Data.(string); ok && s != "" {
			return fmt.Errorf("%s", s)
		}
		return fmt.Errorf("upload failed: status=%s", out.Status)
	}
	return nil
}

func postApplyUpdateJSON(client *http.Client, applyURL, version string) error {
	payload, err := json.Marshal(map[string]string{"version": version})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, applyURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out server.APIResponse
	if json.Unmarshal(body, &out) != nil {
		return fmt.Errorf("parse apply response: %s", strings.TrimSpace(string(body)))
	}
	if out.Status != "success" {
		if s, ok := out.Data.(string); ok && s != "" {
			return fmt.Errorf("%s", s)
		}
		return fmt.Errorf("apply failed: status=%s", out.Status)
	}
	return nil
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
