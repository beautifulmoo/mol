// Package clirest provides HTTP client helpers for maintenance CLIs (Gin API on Server.HTTPPort).
package clirest

import (
	"bytes"
	"context"
	"encoding/json"
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

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/server"
)

// DefaultAPIPrefix is the Gin-proxied API path when -apiprefix is omitted.
const DefaultAPIPrefix = "/maintenance/api/v1"

// DefaultHTTPPort is Server.HTTPPort when not overridden.
const DefaultHTTPPort = 8888

const selfTarget = "self"
const localTarget = "local"
const bundleFormField = "bundle"

// IsLocalTarget reports whether target names this machine (CLI/REPL: self or local).
// HTTP JSON bodies still use "self"; this is only for argv targets.
func IsLocalTarget(target string) bool {
	t := strings.TrimSpace(target)
	return strings.EqualFold(t, selfTarget) || strings.EqualFold(t, localTarget)
}

// NormalizeAPIPrefix returns a path prefix (leading slash, no trailing slash).
// Empty input uses DefaultAPIPrefix.
func NormalizeAPIPrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return DefaultAPIPrefix
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}

func resolveHost(target string) string {
	if IsLocalTarget(target) {
		return "127.0.0.1"
	}
	return strings.TrimSpace(target)
}

// APIBaseURL returns http://host:port{apiPrefix} for API calls (no trailing slash on prefix).
func APIBaseURL(target, apiPrefix string) string {
	host := resolveHost(target)
	prefix := NormalizeAPIPrefix(apiPrefix)
	return "http://" + net.JoinHostPort(host, strconv.Itoa(DefaultHTTPPort)) + prefix
}

func ensureHealthOK(client *http.Client, healthURL, serviceAt string) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, healthURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s (GET %s failed: %v)", serviceAt, healthURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s (GET %s returned HTTP %d: %s)", serviceAt, healthURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &out) != nil || out.Status != "success" {
		return fmt.Errorf("%s (GET %s: unexpected response)", serviceAt, healthURL)
	}
	return nil
}

// DefaultHTTPClient is used by CLIs for REST calls.
func DefaultHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// EnsureServiceRunning checks GET {apiBase}/health and returns an error if the agent HTTP API is unreachable.
func EnsureServiceRunning(client *http.Client, target, apiPrefix string) error {
	if client == nil {
		client = DefaultHTTPClient(5 * time.Second)
	}
	base := APIBaseURL(target, apiPrefix)
	return ensureHealthOK(client, base+"/health", fmt.Sprintf("agent service is not running at %s", base))
}

// ValidateTarget returns an error if target is empty, or remote target is not a valid IP.
func ValidateTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target must not be empty")
	}
	if IsLocalTarget(target) {
		return nil
	}
	if net.ParseIP(target) == nil {
		return fmt.Errorf("remote target must be a valid IP address: %q", target)
	}
	return nil
}

// GetJSON performs GET and decodes a success envelope into dataDest (pointer).
func GetJSON(client *http.Client, url string, dataDest interface{}) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return decodeSuccess(body, dataDest)
}

// PostJSON performs POST with JSON body and decodes success data into dataDest (optional, may be nil).
func PostJSON(client *http.Client, url string, payload interface{}, dataDest interface{}) error {
	var buf bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&buf).Encode(payload); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return decodeSuccess(body, dataDest)
}

func decodeSuccess(body []byte, dataDest interface{}) error {
	var env struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("parse response: %w (body: %s)", err, strings.TrimSpace(string(body)))
	}
	if env.Status != "success" {
		var fail server.APIResponse
		if json.Unmarshal(body, &fail) == nil {
			if s, ok := fail.Data.(string); ok && s != "" {
				return fmt.Errorf("%s", s)
			}
		}
		return fmt.Errorf("request failed: status=%s body=%s", env.Status, strings.TrimSpace(string(body)))
	}
	if dataDest != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, dataDest); err != nil {
			return fmt.Errorf("parse data: %w", err)
		}
	}
	return nil
}

// SelfInfo holds fields from GET /self used by apply-update CLI.
type SelfInfo struct {
	Version      string `json:"version"`
	BuildVariant string `json:"build_variant"`
}

// GetSelf fetches GET {apiBase}/self.
func GetSelf(client *http.Client, target, apiPrefix string) (SelfInfo, error) {
	var out SelfInfo
	base := APIBaseURL(target, apiPrefix)
	if err := GetJSON(client, base+"/self", &out); err != nil {
		return out, err
	}
	return out, nil
}

// GetHostInfo fetches GET {apiBase}/self (host info for the target agent).
func GetHostInfo(client *http.Client, target, apiPrefix string) (discovery.DiscoveryResponse, error) {
	var data discovery.DiscoveryResponse
	base := APIBaseURL(target, apiPrefix)
	if err := GetJSON(client, base+"/self", &data); err != nil {
		return data, err
	}
	return data, nil
}

// UploadBundle posts multipart bundle to POST {apiBase}/upload; returns version key.
func UploadBundle(client *http.Client, target, apiPrefix, bundlePath string) (string, error) {
	resolved, err := ExpandLocalPath(bundlePath)
	if err != nil {
		return "", err
	}
	f, err := os.Open(resolved)
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

	base := APIBaseURL(target, apiPrefix)
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := decodeSuccess(body, &payload); err != nil {
		return "", err
	}
	version := strings.TrimSpace(payload.Version)
	if version == "" {
		return "", fmt.Errorf("upload succeeded but version key is empty")
	}
	return version, nil
}

// ApplyUpdateJSON posts POST {apiBase}/apply-update with version, agent_variant, and reuse_previous_config.
func ApplyUpdateJSON(client *http.Client, target, apiPrefix, version, agentVariant string, reusePreviousConfig bool) (string, error) {
	body := map[string]interface{}{
		"version":               version,
		"ip":                    selfTarget,
		"reuse_previous_config": reusePreviousConfig,
	}
	if strings.TrimSpace(agentVariant) != "" {
		body["agent_variant"] = strings.TrimSpace(agentVariant)
	}
	base := APIBaseURL(target, apiPrefix)
	var msg string
	if err := PostJSON(client, base+"/apply-update", body, &msg); err != nil {
		return "", err
	}
	return msg, nil
}

// ResolveAgentVariantForTarget returns variant for apply-update when -agent-variant flag is empty.
func ResolveAgentVariantForTarget(flagValue, installedVariant string) (string, error) {
	return appmeta.ResolveAgentVariantForApply(flagValue, installedVariant)
}

// ParseAPIPrefixFlag extracts -apiprefix from args (order-independent). Returns remaining positional args.
func ParseAPIPrefixFlag(args []string) (apiPrefix string, rest []string, showHelp bool, err error) {
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			showHelp = true
			i++
		case a == "-apiprefix" || a == "--apiprefix":
			if i+1 >= len(args) {
				return "", nil, false, fmt.Errorf("-apiprefix requires a path argument")
			}
			apiPrefix = args[i+1]
			i += 2
		case strings.HasPrefix(a, "-apiprefix="):
			apiPrefix = strings.TrimSpace(strings.TrimPrefix(a, "-apiprefix="))
			i++
		case strings.HasPrefix(a, "--apiprefix="):
			apiPrefix = strings.TrimSpace(strings.TrimPrefix(a, "--apiprefix="))
			i++
		case strings.HasPrefix(a, "-"):
			return "", nil, false, fmt.Errorf("unknown flag %q", a)
		default:
			rest = append(rest, a)
			i++
		}
	}
	return apiPrefix, rest, showHelp, nil
}
