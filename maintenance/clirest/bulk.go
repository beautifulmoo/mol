package clirest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultMaintenancePort is Maintenance.MaintenancePort when -maintenance-port is omitted.
const DefaultMaintenancePort = 8889

// BulkPushHost is one remote target for bulk maintenance APIs (push-local-all, restart-all, …).
type BulkPushHost struct {
	PrimaryIP string   `json:"primary_ip"`
	Hostname  string   `json:"hostname"`
	CPUUUID   string   `json:"cpu_uuid"`
	IPs       []string `json:"ips"`
}

// MaintenanceBaseURL returns http://127.0.0.1:{port}{apiPrefix} for the local orchestrator maintenance HTTP server.
func MaintenanceBaseURL(apiPrefix string, maintenancePort int) string {
	if maintenancePort <= 0 {
		maintenancePort = DefaultMaintenancePort
	}
	prefix := NormalizeAPIPrefix(apiPrefix)
	return "http://127.0.0.1:" + strconv.Itoa(maintenancePort) + prefix
}

// EnsureMaintenanceRunning checks GET {maintenanceBase}/health.
func EnsureMaintenanceRunning(client *http.Client, apiPrefix string, maintenancePort int) error {
	if client == nil {
		client = DefaultHTTPClient(5 * time.Second)
	}
	base := MaintenanceBaseURL(apiPrefix, maintenancePort)
	return ensureHealthOK(client, base+"/health", fmt.Sprintf("maintenance service is not running at %s", base))
}

func postBulkNDJSON(client *http.Client, apiPrefix string, maintenancePort int, apiPath string, body map[string]interface{}, onEvent func(map[string]interface{}) error) error {
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	base := MaintenanceBaseURL(apiPrefix, maintenancePort)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+apiPath, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s HTTP %d: %s", strings.TrimPrefix(apiPath, "/"), resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "ndjson") {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: expected NDJSON stream (got %q): %s", strings.TrimPrefix(apiPath, "/"), ct, strings.TrimSpace(string(bodyBytes)))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			return fmt.Errorf("parse NDJSON line: %w", err)
		}
		if onEvent != nil {
			if err := onEvent(evt); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func postBulkHostsNDJSON(client *http.Client, apiPrefix string, maintenancePort int, apiPath string, hosts []BulkPushHost, onEvent func(map[string]interface{}) error) error {
	return postBulkNDJSON(client, apiPrefix, maintenancePort, apiPath, map[string]interface{}{"hosts": hosts}, onEvent)
}

// PushLocalConfigAll posts POST {base}/current-config/push-local-all and invokes onEvent per NDJSON line.
func PushLocalConfigAll(client *http.Client, apiPrefix string, maintenancePort int, hosts []BulkPushHost, onEvent func(map[string]interface{}) error) error {
	return postBulkHostsNDJSON(client, apiPrefix, maintenancePort, "/current-config/push-local-all", hosts, onEvent)
}

// RestartAllRemotes posts POST {base}/service-control/restart-all and invokes onEvent per NDJSON line.
func RestartAllRemotes(client *http.Client, apiPrefix string, maintenancePort int, hosts []BulkPushHost, onEvent func(map[string]interface{}) error) error {
	return postBulkHostsNDJSON(client, apiPrefix, maintenancePort, "/service-control/restart-all", hosts, onEvent)
}

// ApplyUpdateAllOptions configures POST {base}/apply-update-all.
type ApplyUpdateAllOptions struct {
	Version             string
	AgentVariant        string
	ReusePreviousConfig bool
}

// ApplyUpdateAll posts POST {base}/apply-update-all and invokes onEvent per NDJSON line.
func ApplyUpdateAll(client *http.Client, apiPrefix string, maintenancePort int, hosts []BulkPushHost, opts ApplyUpdateAllOptions, onEvent func(map[string]interface{}) error) error {
	body := map[string]interface{}{
		"hosts":                 hosts,
		"version":               strings.TrimSpace(opts.Version),
		"reuse_previous_config": opts.ReusePreviousConfig,
	}
	if v := strings.TrimSpace(opts.AgentVariant); v != "" {
		body["agent_variant"] = v
	}
	return postBulkNDJSON(client, apiPrefix, maintenancePort, "/apply-update-all", body, onEvent)
}

// RollbackAllRemotes posts POST {base}/versions/rollback-all and invokes onEvent per NDJSON line.
func RollbackAllRemotes(client *http.Client, apiPrefix string, maintenancePort int, hosts []BulkPushHost, onEvent func(map[string]interface{}) error) error {
	return postBulkHostsNDJSON(client, apiPrefix, maintenancePort, "/versions/rollback-all", hosts, onEvent)
}

// UploadBundleMaintenance posts multipart bundle to local maintenance POST {base}/upload; returns version key.
func UploadBundleMaintenance(client *http.Client, apiPrefix string, maintenancePort int, bundlePath string) (string, error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(bundleFormField, filepath.Base(bundlePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	base := MaintenanceBaseURL(apiPrefix, maintenancePort)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := decodeSuccess(respBody, &payload); err != nil {
		return "", err
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return "", fmt.Errorf("upload succeeded but version key is empty")
	}
	return version, nil
}

// FormatBulkHostLabel returns "hostname (ip)" for progress output.
func FormatBulkHostLabel(hostname, ip string) string {
	hn := strings.TrimSpace(hostname)
	ip = strings.TrimSpace(ip)
	if hn == "" {
		hn = ip
	}
	if ip == "" || hn == ip {
		return hn
	}
	return hn + " (" + ip + ")"
}
