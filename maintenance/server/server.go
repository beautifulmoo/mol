package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
	"contrabass-agent/maintenance/hostinfoapi"
	"contrabass-agent/maintenance/server/remoteregistry"
	"contrabass-agent/maintenance/svcstatus"
	"contrabass-agent/maintenance/versionsapi"
)

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}

func dirHasAgentBinary(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, appmeta.BinaryName))
	return err == nil
}

// elfMagic is the first 4 bytes of an ELF executable.
var elfMagic = []byte{0x7f, 'E', 'L', 'F'}

func isELFExecutable(header []byte) bool {
	return len(header) >= 4 && header[0] == elfMagic[0] && header[1] == elfMagic[1] && header[2] == elfMagic[2] && header[3] == elfMagic[3]
}

// VersionKeyFromAgentBinary runs binPath --version, or on failure agent --version, and returns the version key after "<BinaryName> " (same as POST /upload validation). Exported for CLI.
func VersionKeyFromAgentBinary(binPath string) (string, error) {
	return versionKeyFromAgentBinary(binPath)
}

// BuildVariantFromAgentBinary runs the same --version invocations and returns "(control)" or "(compute)" from the line, or "" if absent.
func BuildVariantFromAgentBinary(binPath string) (string, error) {
	line, err := versionsapi.AgentVersionLine(binPath)
	if err != nil {
		return "", err
	}
	return appmeta.ParseBuildVariantFromVersionLine(line), nil
}

// versionKeyFromAgentBinary tries root --version first (legacy / transitional updates), then agent --version.
func versionKeyFromAgentBinary(binPath string) (string, error) {
	line, err := versionsapi.AgentVersionLine(binPath)
	if err != nil {
		return "", fmt.Errorf("not a valid executable (--version: %v)", err)
	}
	want := appmeta.BinaryName + " "
	if !strings.HasPrefix(line, want) {
		return "", fmt.Errorf("prefix want %q, got %q", want, line)
	}
	key := strings.TrimSpace(strings.TrimPrefix(line, want))
	if idx := strings.Index(key, " ("); idx >= 0 {
		key = strings.TrimSpace(key[:idx])
	}
	if key == "" {
		return "", fmt.Errorf("empty version key")
	}
	if err := agentcfg.ValidateVersionKeyPath(key); err != nil {
		return "", err
	}
	return key, nil
}

// validateAgentBinary runs the same version checks as bundle upload (root --version, then agent --version).
func validateAgentBinary(binPath string) error {
	_, err := versionKeyFromAgentBinary(binPath)
	return err
}

const (
	sseContentType = "text/event-stream"
	sseNoCache     = "no-cache"
	sseKeepAlive   = "keep-alive"
)

// APIResponse is the common API response shape (status + data).
type APIResponse struct {
	Status string      `json:"status"` // "success" or "fail"
	Data   interface{} `json:"data"`
}

// Server runs HTTP server (static + API).
type Server struct {
	webPrefix            string
	apiPrefix            string
	webFS                fs.FS
	discovery            *discovery.Discovery
	getHostInfo          func() (hostinfo.Info, error)
	version              string
	servicePort          int
	remoteProxyPort      int
	discoveryServiceName string
	systemctlServiceName string
	deployBase           string
	installPrefix        string // contrabass-moleU 설치 경로 prefix (versions/ 기준). 비면 deployBase 사용
	sshPort              int
	sshUser              string
	maxUploadBytes         int64 // POST /upload & multipart apply-update: max body (tar.gz bundle field)
	allowSameVersionUpdate bool
	buildVariant           string
	remoteHealthIntervalSec  int
	remoteHealthTimeoutSec   int
	remoteHealthThreshold    int
	remoteHealthJitterSec    int
	remoteRegistry           *remoteregistry.Registry
}

// Config for Server.
type Config struct {
	WebPrefix            string
	APIPrefix            string
	WebFS                fs.FS
	Discovery            *discovery.Discovery
	GetHostInfo          func() (hostinfo.Info, error)
	Version              string
	ServicePort          int
	RemoteProxyPort      int // external proxy port (Gin). should be Server.HTTPPort
	DiscoveryServiceName string
	SystemctlServiceName string
	DeployBase           string
	InstallPrefix        string // contrabass-moleU 설치 경로 prefix. 비면 DeployBase 사용 (versions 목록·삭제, installer)
	SSHPort              int    // for remote service start/stop via SSH (default 22)
	SSHUser              string // SSH user for remote (default "root")
	MaxUploadBytes         int    // 0 or omit → agentcfg.DefaultMaxUploadBytes (64<<20); max multipart body for upload / multipart apply-update
	AllowSameVersionUpdate bool   // when true, same version can be re-applied (for testing/rollback)
	BuildVariant           string // "control", "compute", or "" — from ldflags at build time
	RemoteHealthCheckIntervalSeconds  int
	RemoteHealthCheckTimeoutSeconds   int
	RemoteHealthCheckFailureThreshold int
	RemoteHealthCheckJitterSeconds    int
}

// New creates a Server.
func New(cfg Config) *Server {
	s := &Server{
		webPrefix:            strings.TrimSuffix(cfg.WebPrefix, "/"),
		apiPrefix:            strings.TrimSuffix(cfg.APIPrefix, "/"),
		webFS:                cfg.WebFS,
		discovery:            cfg.Discovery,
		getHostInfo:          cfg.GetHostInfo,
		version:              cfg.Version,
		servicePort:          cfg.ServicePort,
		remoteProxyPort:      cfg.RemoteProxyPort,
		discoveryServiceName: cfg.DiscoveryServiceName,
		systemctlServiceName: cfg.SystemctlServiceName,
		deployBase:           strings.TrimSuffix(cfg.DeployBase, "/"),
		installPrefix:        strings.TrimSuffix(cfg.InstallPrefix, "/"),
		sshPort:              cfg.SSHPort,
		sshUser:              cfg.SSHUser,
		maxUploadBytes:         agentcfg.ClampMaxUploadBytes(cfg.MaxUploadBytes),
		allowSameVersionUpdate:  cfg.AllowSameVersionUpdate,
		buildVariant:            cfg.BuildVariant,
		remoteHealthIntervalSec:  cfg.RemoteHealthCheckIntervalSeconds,
		remoteHealthTimeoutSec:   cfg.RemoteHealthCheckTimeoutSeconds,
		remoteHealthThreshold:    cfg.RemoteHealthCheckFailureThreshold,
		remoteHealthJitterSec:    cfg.RemoteHealthCheckJitterSeconds,
	}
	if s.installPrefix == "" {
		s.installPrefix = s.deployBase
	}
	if s.remoteHealthIntervalSec <= 0 {
		s.remoteHealthIntervalSec = 10
	}
	if s.remoteHealthTimeoutSec <= 0 {
		s.remoteHealthTimeoutSec = 2
	}
	if s.remoteHealthThreshold <= 0 {
		s.remoteHealthThreshold = 3
	}
	if s.remoteHealthJitterSec < 0 {
		s.remoteHealthJitterSec = 0
	}
	s.remoteRegistry = remoteregistry.New(s.remoteHealthThreshold)
	return s
}

// DiscoveredRemotes returns a snapshot of volatile in-memory discovered remote agents.
func (s *Server) DiscoveredRemotes() []remoteregistry.Remote {
	if s.remoteRegistry == nil {
		return nil
	}
	return s.remoteRegistry.List()
}

func (s *Server) remoteBaseURL(ip string) (string, error) {
	port := s.remoteProxyPort
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("Server.HTTPPort must be 1..65535")
	}
	return "http://" + ip + ":" + strconv.Itoa(port), nil
}

// fetchRemoteVersionKey returns the remote agent's version key from GET {APIPrefix}/self.
func (s *Server) fetchRemoteVersionKey(ip string) (string, error) {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return "", err
	}
	u := baseURL + s.apiPrefix + "/self"
	resp, err := remoteHTTPClient.Get(u)
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
		return "", err
	}
	if out.Status != "success" {
		return "", fmt.Errorf("remote self: status %q", out.Status)
	}
	return strings.TrimSpace(out.Data.Version), nil
}

// looksLikeBrowser returns true if the request is likely from a browser (e.g. Accept: text/html or User-Agent: Mozilla/...).
// Used to redirect GET / to /web/ only for browsers; curl/Postman get 404.
func looksLikeBrowser(r *http.Request) bool {
	if ah := r.Header.Get("Accept"); ah != "" && strings.Contains(strings.ToLower(ah), "text/html") {
		return true
	}
	ua := r.Header.Get("User-Agent")
	return strings.Contains(ua, "Mozilla") || strings.Contains(ua, "Chrome") || strings.Contains(ua, "Safari") || strings.Contains(ua, "Firefox") || strings.Contains(ua, "Edg")
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if looksLikeBrowser(r) {
		http.Redirect(w, r, s.webPrefix+"/", http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/version" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	v := s.version
	if v == "" {
		v = "0.0.0-0"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(appmeta.BinaryName + " " + v))
}

// handleClientRuntime serves a tiny script so the embedded web UI uses Maintenance.APIPrefix (not a hardcoded /api/v1).
func (s *Server) handleClientRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	quoted, err := json.Marshal(s.apiPrefix)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	healthCfg := map[string]int{
		"intervalSec":       s.remoteHealthIntervalSec,
		"timeoutSec":        s.remoteHealthTimeoutSec,
		"failureThreshold":  s.remoteHealthThreshold,
		"jitterSec":         s.remoteHealthJitterSec,
	}
	healthJSON, err := json.Marshal(healthCfg)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	discoveryTimeoutSec := 10
	if s.discovery != nil {
		discoveryTimeoutSec = s.discovery.DefaultTimeoutSeconds()
	}
	discoveryCfg := map[string]int{"timeoutSec": discoveryTimeoutSec}
	discoveryJSON, err := json.Marshal(discoveryCfg)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "window.__CONTRABASS_API_PREFIX__=%s;\n", quoted)
	fmt.Fprintf(w, "window.__CONTRABASS_REMOTE_HEALTH__=%s;\n", string(healthJSON))
	fmt.Fprintf(w, "window.__CONTRABASS_DISCOVERY__=%s;\n", string(discoveryJSON))
}

// handleHealth returns a minimal JSON liveness payload for GET {APIPrefix}/health (remote agents use the same path via Gin proxy).
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	s.send(w, "success", map[string]interface{}{"ok": true}, http.StatusOK)
}

// handleRemoteHealthCheck proxies GET .../health to the discovered host's Server.HTTPPort (HTTP API, not UDP).
func (s *Server) handleRemoteHealthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip == "" || ip == "self" {
		s.send(w, "fail", "ip query parameter is required", http.StatusOK)
		return
	}
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		s.remoteRegistry.RecordHealthCheck(ip, false, err.Error())
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	u := baseURL + s.apiPrefix + "/health"
	timeout := time.Duration(s.remoteHealthTimeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		s.remoteRegistry.RecordHealthCheck(ip, false, err.Error())
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.remoteRegistry.RecordHealthCheck(ip, false, err.Error())
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if err != nil {
		s.remoteRegistry.RecordHealthCheck(ip, false, err.Error())
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		s.remoteRegistry.RecordHealthCheck(ip, false, msg)
		s.send(w, "fail", msg, http.StatusOK)
		return
	}
	var out APIResponse
	if json.Unmarshal(body, &out) == nil && out.Status == "success" {
		s.remoteRegistry.RecordHealthCheck(ip, true, "")
		s.send(w, "success", map[string]interface{}{"ok": true}, http.StatusOK)
		return
	}
	s.remoteRegistry.RecordHealthCheck(ip, false, "health response is not in the expected format")
	s.send(w, "fail", "health response is not in the expected format", http.StatusOK)
}

// Handler returns http.Handler that serves web and API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/", s.handleRoot)
	// API
	mux.HandleFunc(s.apiPrefix+"/self", s.handleSelf)
	mux.HandleFunc(s.apiPrefix+"/health", s.handleHealth)
	mux.HandleFunc(s.apiPrefix+"/remote-health-check", s.handleRemoteHealthCheck)
	mux.HandleFunc(s.apiPrefix+"/host-info", s.handleHostInfo)
	mux.HandleFunc(s.apiPrefix+"/discovery", s.handleDiscovery)
	mux.HandleFunc(s.apiPrefix+"/discovery/stream", s.handleDiscoveryStream)
	mux.HandleFunc(s.apiPrefix+"/service-status", s.handleServiceStatus)
	mux.HandleFunc(s.apiPrefix+"/service-control/restart-all", s.handleServiceRestartAll)
	mux.HandleFunc(s.apiPrefix+"/apply-update-all", s.handleApplyUpdateAll)
	mux.HandleFunc(s.apiPrefix+"/versions/rollback-all", s.handleRollbackAll)
	mux.HandleFunc(s.apiPrefix+"/service-control", s.handleServiceControl)
	mux.HandleFunc(s.apiPrefix+"/upload", s.handleUpload)
	mux.HandleFunc(s.apiPrefix+"/upload/remove", s.handleRemoveUpload)
	mux.HandleFunc(s.apiPrefix+"/update-status", s.handleUpdateStatus)
	mux.HandleFunc(s.apiPrefix+"/apply-update", s.handleApplyUpdate)
	mux.HandleFunc(s.apiPrefix+"/update-log", s.handleUpdateLog)
	mux.HandleFunc(s.apiPrefix+"/current-config", s.handleCurrentConfig)
	mux.HandleFunc(s.apiPrefix+"/current-config/push-local", s.handleCurrentConfigPushLocal)
	mux.HandleFunc(s.apiPrefix+"/current-config/push-local-all", s.handleCurrentConfigPushLocalAll)
	mux.HandleFunc(s.apiPrefix+"/discovered-remotes", s.handleDiscoveredRemotes)
	mux.HandleFunc(s.apiPrefix+"/versions/list", s.handleVersionsList)
	mux.HandleFunc(s.apiPrefix+"/versions/remove", s.handleVersionsRemove)
	mux.HandleFunc(s.apiPrefix+"/versions/switch-current", s.handleVersionsSwitchCurrent)
	mux.HandleFunc(s.apiPrefix+"/versions/rollback", s.handleVersionsRollback)
	// Web (static) — register client-runtime before the strip-prefix file server so it is not shadowed.
	mux.HandleFunc(s.webPrefix+"/client-runtime.js", s.handleClientRuntime)
	webHandler := http.StripPrefix(s.webPrefix, http.FileServer(http.FS(s.webFS)))
	mux.Handle(s.webPrefix+"/", webHandler)
	return mux
}

func (s *Server) handleSelf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	info, err := s.getHostInfo()
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusInternalServerError)
		return
	}
	data := hostinfoapi.SelfDiscoveryResponse(info, hostinfoapi.SelfDiscoveryMeta{
		Version:              s.version,
		ServicePort:          s.servicePort,
		DiscoveryServiceName: s.discoveryServiceName,
		BuildVariant:         s.buildVariant,
	})
	s.send(w, "success", data, http.StatusOK)
}

func (s *Server) handleHostInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip == "" || ip == "self" {
		s.handleSelf(w, r)
		return
	}
	resp, err := hostinfoapi.RemoteHostInfo(s.discovery, ip)
	if err != nil {
		log.Printf("discovery: ERROR: DoDiscoveryUnicast(ip=%s) failed: %v", ip, err)
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.remoteRegistry.UpsertFromDiscovery(*resp)
	s.send(w, "success", resp, http.StatusOK)
}

// Query params for GET .../discovery and .../discovery/stream:
//   exclude_self — true/1/yes/on: omit this host from results; omitted or false: include self ("self": true in JSON when applicable).
//   exclude-self — same as exclude_self (optional alias).
//   timeout      — integer seconds (1–600); omitted: Maintenance.DiscoveryTimeoutSeconds (0 or unset in YAML → 10s).
func requestQueryValues(r *http.Request) url.Values {
	if r.URL != nil && r.URL.RawQuery != "" {
		if q, err := url.ParseQuery(r.URL.RawQuery); err == nil {
			return q
		}
	}
	if r.RequestURI != "" {
		if i := strings.IndexByte(r.RequestURI, '?'); i >= 0 {
			if q, err := url.ParseQuery(r.RequestURI[i+1:]); err == nil {
				return q
			}
		}
	}
	return url.Values{}
}

func parseDiscoveryRunOptions(r *http.Request) (discovery.DiscoveryRunOptions, error) {
	q := requestQueryValues(r)
	var opts discovery.DiscoveryRunOptions
	v := strings.TrimSpace(q.Get("exclude_self"))
	if v == "" {
		v = strings.TrimSpace(q.Get("exclude-self"))
	}
	if v != "" {
		opts.ExcludeSelf = parseQueryBoolTrue(v)
	}
	if v := strings.TrimSpace(q.Get("timeout")); v != "" {
		sec, err := strconv.Atoi(v)
		if err != nil {
			return opts, fmt.Errorf("timeout must be an integer (seconds)")
		}
		if sec < 1 || sec > 600 {
			return opts, fmt.Errorf("timeout must be between 1 and 600 seconds")
		}
		opts.Timeout = time.Duration(sec) * time.Second
	}
	return opts, nil
}

func parseQueryBoolTrue(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

func parseReusePreviousConfig(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return true
	}
	if s == "0" || s == "false" || s == "no" || s == "off" {
		return false
	}
	return parseQueryBoolTrue(s)
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	opts, err := parseDiscoveryRunOptions(r)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusBadRequest)
		return
	}
	list, err := s.discovery.DoDiscovery(opts)
	if err != nil {
		log.Printf("discovery: ERROR: DoDiscovery failed: %v", err)
		s.send(w, "fail", err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []discovery.DiscoveryResponse{}
	}
	for _, host := range list {
		s.remoteRegistry.UpsertFromDiscovery(host)
	}
	log.Printf("discovery API: returning %d host(s)", len(list))
	s.send(w, "success", list, http.StatusOK)
}

func (s *Server) handleDiscoveryStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	opts, err := parseDiscoveryRunOptions(r)
	if err != nil {
		w.Header().Set("Content-Type", sseContentType)
		w.Header().Set("Cache-Control", sseNoCache)
		w.Header().Set("Connection", sseKeepAlive)
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		payload, _ := json.Marshal(map[string]string{"message": err.Error()})
		if _, werr := fmt.Fprintf(w, "event: discoveryfail\ndata: %s\n\n", payload); werr != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	ch, err := s.discovery.DoDiscoveryStream(opts)
	if err != nil {
		// EventSource cannot read JSON error bodies on non-2xx; send a one-line SSE error event with 200 OK.
		log.Printf("discovery: ERROR: DoDiscoveryStream failed: %v", err)
		w.Header().Set("Content-Type", sseContentType)
		w.Header().Set("Cache-Control", sseNoCache)
		w.Header().Set("Connection", sseKeepAlive)
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		payload, _ := json.Marshal(map[string]string{"message": err.Error()})
		if _, werr := fmt.Fprintf(w, "event: discoveryfail\ndata: %s\n\n", payload); werr != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", sseContentType)
	w.Header().Set("Cache-Control", sseNoCache)
	w.Header().Set("Connection", sseKeepAlive)
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	enc := json.NewEncoder(w)
	for host := range ch {
		s.remoteRegistry.UpsertFromDiscovery(host)
		if _, err := w.Write([]byte("data: ")); err != nil {
			return
		}
		if err := enc.Encode(host); err != nil {
			return
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	if _, err := w.Write([]byte("event: done\ndata: {}\n\n")); err != nil {
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	svcName := s.systemctlServiceName
	if svcName == "" {
		svcName = "contrabass-mole.service"
	}
	if ip != "" && ip != "self" {
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote service-status request failed: "+err.Error(), http.StatusOK)
			return
		}
		url := baseURL + s.apiPrefix + "/service-status"
		resp, err := remoteHTTPClient.Get(url)
		if err != nil {
			s.send(w, "fail", "remote service-status request failed: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "failed to parse remote response", http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}
	output, err := svcstatus.GetLocal(svcName)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", map[string]string{"output": output}, http.StatusOK)
}

// serviceControlRequest is the JSON body for POST /api/v1/service-control.
type serviceControlRequest struct {
	IP     string `json:"ip"`
	Action string `json:"action"` // "start", "stop", or "restart"
}

func (s *Server) handleServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	var req serviceControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(req.IP)
	action := strings.TrimSpace(strings.ToLower(req.Action))
	if action != "start" && action != "stop" && action != "restart" {
		s.send(w, "fail", "action must be start, stop, or restart", http.StatusBadRequest)
		return
	}
	svcName := s.systemctlServiceName
	if svcName == "" {
		svcName = "contrabass-mole.service"
	}
	if ip != "" && ip != "self" {
		if action == "restart" {
			// 재시작만 원격 에이전트 API 호출로 처리 (SSH 키 불필요). 원격에서 systemctl restart 수행.
			baseURL, err := s.remoteBaseURL(ip)
			if err != nil {
				s.send(w, "fail", "remote restart request failed: "+err.Error(), http.StatusOK)
				return
			}
			baseURL = baseURL + s.apiPrefix + "/service-control"
			payload, _ := json.Marshal(map[string]string{"ip": "self", "action": "restart"})
			req, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			resp, err := remoteHTTPClient.Do(req)
			if err != nil {
				s.send(w, "fail", "remote restart request failed: "+err.Error(), http.StatusOK)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var out APIResponse
			if json.Unmarshal(body, &out) != nil {
				s.send(w, "fail", "failed to parse remote response", http.StatusOK)
				return
			}
			s.send(w, out.Status, out.Data, http.StatusOK)
			return
		}
		// 시작/중지는 SSH로 실행 (서비스 중지 시 API 호출 불가)
		sshPort := s.sshPort
		if sshPort <= 0 {
			sshPort = 22
		}
		sshUser := s.sshUser
		if sshUser == "" {
			sshUser = "root"
		}
		err := svcstatus.RunRemote(ip, sshUser, sshPort, svcName, action)
		if err != nil {
			s.send(w, "fail", "remote SSH control failed: "+err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", nil, http.StatusOK)
		return
	}
	var err error
	switch action {
	case "start":
		err = svcstatus.StartLocal(svcName)
	case "stop":
		err = svcstatus.StopLocal(svcName)
	default:
		err = svcstatus.RestartLocal(svcName)
	}
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", nil, http.StatusOK)
}

// stagingDir returns deploy_base/staging/<version>. Staging is never the running path, so no "text file busy".
func (s *Server) stagingDir(base, version string) string {
	return filepath.Join(base, "staging", version)
}

// clearStaging removes the entire deploy_base/staging/ directory so that upload replaces all staging content with the new version only.
func (s *Server) clearStaging(base string) {
	stagingParent := filepath.Join(base, "staging")
	_ = os.RemoveAll(stagingParent)
}

// versionsDir returns base/versions/<version> (the running path).
func (s *Server) versionsDir(base, version string) string {
	return filepath.Join(base, "versions", version)
}

// versionsBase returns the base path for versions/ (install_prefix or deploy_base). Used for list/remove and installer.
func (s *Server) versionsBase() string {
	return versionsapi.VersionsBaseFromParts(s.installPrefix, s.deployBase)
}

// resolveVersionDir returns the directory that contains the agent binary + config for this version: staging first (under base/deploy), then versions/ under versionsBase() (same tree as GET /versions/list).
func (s *Server) resolveVersionDir(base, version string) (string, bool) {
	stg := s.stagingDir(base, version)
	if versionsapi.DirHasStagedAgents(stg) {
		return stg, true // from staging
	}
	ver := filepath.Join(s.versionsBase(), "versions", version)
	if versionsapi.DirHasStagedAgents(ver) {
		return ver, false // from versions
	}
	return "", false
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		s.send(w, "fail", "request is not multipart or body could not be read", http.StatusBadRequest)
		return
	}
	// multipart.Reader는 NextPart() 시 이전 Part를 Close()하며 본문을 버린다. 번들은 루프 안에서 즉시 읽어야 한다.
	var bundleData []byte
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.send(w, "fail", "request too large or multipart read failed", http.StatusBadRequest)
			return
		}
		switch part.FormName() {
		case uploadBundleField:
			buf := new(bytes.Buffer)
			_, err := io.Copy(buf, io.LimitReader(part, s.maxUploadBytes))
			_ = part.Close()
			if err != nil {
				s.send(w, "fail", "failed to read bundle part: "+err.Error(), http.StatusBadRequest)
				return
			}
			bundleData = buf.Bytes()
		default:
			_, _ = io.Copy(io.Discard, part)
			part.Close()
		}
	}
	if len(bundleData) == 0 {
		s.send(w, "fail", "bundle file required (multipart field \""+uploadBundleField+"\", tar.gz)", http.StatusBadRequest)
		return
	}

	pb, err := prepareAgentBundle(base, bytes.NewReader(bundleData), s.maxUploadBytes)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = os.RemoveAll(pb.WorkDir) }()

	s.clearStaging(base)

	finalDir := s.stagingDir(base, pb.VersionKey)
	if err := os.MkdirAll(filepath.Join(base, "staging"), 0755); err != nil {
		s.send(w, "fail", "failed to create staging directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		s.send(w, "fail", "failed to create staging version directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := StagePreparedBundle(finalDir, pb); err != nil {
		_ = os.RemoveAll(finalDir)
		s.send(w, "fail", err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.WriteFile(filepath.Join(finalDir, StagedBundleFileName), bundleData, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		s.send(w, "fail", "failed to save uploaded bundle: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("upload: version %s -> %s (staging)", pb.VersionKey, finalDir)
	s.send(w, "success", map[string]string{"version": pb.VersionKey}, http.StatusOK)
}

func (s *Server) handleRemoveUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		s.send(w, "fail", "version is required", http.StatusBadRequest)
		return
	}
	if err := agentcfg.ValidateVersionKeyPath(version); err != nil {
		s.send(w, "fail", "version contains invalid characters", http.StatusBadRequest)
		return
	}
	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	stagingParent := filepath.Join(base, "staging")
	stagingVersionDir := filepath.Join(stagingParent, version)
	clean := filepath.Clean(stagingVersionDir)
	rel, relErr := filepath.Rel(stagingParent, clean)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, "..") || clean == stagingParent {
		s.send(w, "fail", "invalid version path", http.StatusBadRequest)
		return
	}
	if err := os.RemoveAll(stagingVersionDir); err != nil {
		s.send(w, "fail", "delete failed: "+err.Error(), http.StatusOK)
		return
	}
	log.Printf("upload/remove: version %s removed from staging %s", version, stagingVersionDir)
	s.send(w, "success", "version "+version+" removed from staging.", http.StatusOK)
}

// remoteHTTPClient is used to call another agent's upload/apply APIs (no SSH/SCP).
var remoteHTTPClient = &http.Client{Timeout: 300 * time.Second}

// postUploadToTarget POSTs to the remote upload API. If versionDir contains StagedBundleFileName (saved at
// POST /upload), that file is sent unchanged; otherwise a minimal tar.gz is built from binary + config (legacy).
func (s *Server) postUploadToTarget(ctx context.Context, baseURL, apiPrefix, versionDir string) error {
	staged := filepath.Join(versionDir, StagedBundleFileName)
	if fi, err := os.Stat(staged); err == nil && !fi.IsDir() && fi.Size() > 0 {
		return s.postUploadBundlePath(ctx, baseURL, apiPrefix, staged)
	}
	controlPath := filepath.Join(versionDir, appmeta.BundleAgentControlName)
	computePath := filepath.Join(versionDir, appmeta.BundleAgentComputeName)
	configPath := filepath.Join(versionDir, appmeta.ConfigFileName)
	tmp, err := os.CreateTemp("", "remote-bundle-*.tar.gz")
	if err != nil {
		return fmt.Errorf("temp bundle: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	var packErr error
	v2Ready := false
	if fi, err := os.Stat(controlPath); err == nil && !fi.IsDir() {
		if fi2, err2 := os.Stat(computePath); err2 == nil && !fi2.IsDir() {
			v2Ready = true
			packErr = writeBundleTarGz(tmp, controlPath, computePath, configPath)
		}
	}
	if !v2Ready {
		binPath := filepath.Join(versionDir, appmeta.BinaryName)
		if fi, err := os.Stat(binPath); err != nil || fi.IsDir() {
			return fmt.Errorf("bundle rebuild: missing %s or %s/%s", appmeta.BinaryName, appmeta.BundleAgentControlName, appmeta.BundleAgentComputeName)
		}
		packErr = writeBundleTarGzLegacy(tmp, binPath, configPath)
	}
	if packErr != nil {
		_ = tmp.Close()
		return packErr
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return s.postUploadBundlePath(ctx, baseURL, apiPrefix, tmpPath)
}

// postUploadBundlePath sends bundlePath as multipart field "bundle" to POST .../upload (in-memory body; suitable for typical bundle sizes).
func (s *Server) postUploadBundlePath(ctx context.Context, baseURL, apiPrefix, bundlePath string) error {
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		return fmt.Errorf("read bundle: %w", err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(uploadBundleField, "bundle.tar.gz")
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, bytes.NewReader(raw)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	uploadURL := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(apiPrefix, "/") + "/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("remote upload request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Status string      `json:"status"`
		Data   interface{} `json:"data"`
	}
	_ = json.Unmarshal(body, &out)
	if out.Status != "success" {
		msg := "remote upload failed"
		if s, ok := out.Data.(string); ok && s != "" {
			msg = s
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// postApplyUpdateToTarget tells the target agent to apply the given version from its staging (ip=self).
// runUpdateViaEmbeddedScript prepares the version tree and starts embedded update.sh (apply-update / switch-current local).
func (s *Server) runUpdateViaEmbeddedScript(base, version, agentVariant string, reusePreviousConfig bool) error {
	return versionsapi.RunSwitchCurrentWithRootsVariant(base, s.installPrefix, s.deployBase, version, agentVariant, s.buildVariant, reusePreviousConfig)
}

func (s *Server) postApplyUpdateToTarget(ctx context.Context, baseURL, apiPrefix, version, agentVariant string, reusePreviousConfig bool) (status string, data interface{}, err error) {
	applyURL := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(apiPrefix, "/") + "/apply-update"
	if _, err := appmeta.ParseAgentVariant(agentVariant); err != nil {
		return "", nil, err
	}
	payload, err := json.Marshal(map[string]interface{}{
		"version":               version,
		"ip":                    "self",
		"agent_variant":         agentVariant,
		"reuse_previous_config": reusePreviousConfig,
	})
	if err != nil {
		return "", nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, applyURL, bytes.NewReader(payload))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("remote apply request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Status string      `json:"status"`
		Data   interface{} `json:"data"`
	}
	_ = json.Unmarshal(body, &out)
	return out.Status, out.Data, nil
}

func (s *Server) fetchRemoteCurrentConfigContent(ctx context.Context, baseURL, apiPrefix string) (string, error) {
	u := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(apiPrefix, "/") + "/current-config"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("remote current-config request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse remote current-config response: %w", err)
	}
	if out.Status != "success" {
		msg := "remote current-config failed"
		var errStr string
		if json.Unmarshal(out.Data, &errStr) == nil && errStr != "" {
			msg = errStr
		}
		return "", fmt.Errorf("%s", msg)
	}
	var wrapped struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(out.Data, &wrapped); err != nil {
		return "", fmt.Errorf("parse remote current-config content: %w", err)
	}
	return wrapped.Content, nil
}

func (s *Server) injectRemoteReuseConfigIntoVersionDir(ctx context.Context, baseURL, apiPrefix, versionDir string) error {
	content, err := s.fetchRemoteCurrentConfigContent(ctx, baseURL, apiPrefix)
	if err != nil {
		return err
	}
	if err := versionsapi.OverwriteVersionDirConfig(versionDir, content); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(versionDir, StagedBundleFileName))
	return nil
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}

	// 원격 전용: multipart(실행 파일+config+ip) → 원격 업로드 API로 전송 후 원격 apply-update API 호출 (로컬 스테이징·SCP 미사용)
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBytes)
		mr, err := r.MultipartReader()
		if err != nil {
			s.send(w, "fail", "multipart parse failed", http.StatusBadRequest)
			return
		}
		var remoteIP string
		var bundleData []byte
		agentVariant := appmeta.AgentVariantControl
		reusePreviousConfig := true
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				s.send(w, "fail", "request too large or multipart read failed", http.StatusBadRequest)
				return
			}
			switch part.FormName() {
			case "ip":
				b, rerr := io.ReadAll(io.LimitReader(part, 256))
				if rerr != nil {
					part.Close()
					s.send(w, "fail", "multipart read failed", http.StatusBadRequest)
					return
				}
				_ = part.Close()
				remoteIP = strings.TrimSpace(string(b))
			case uploadBundleField:
				buf := new(bytes.Buffer)
				_, err := io.Copy(buf, io.LimitReader(part, s.maxUploadBytes))
				_ = part.Close()
				if err != nil {
					s.send(w, "fail", "failed to read bundle part: "+err.Error(), http.StatusBadRequest)
					return
				}
				bundleData = buf.Bytes()
			case "agent_variant":
				b, rerr := io.ReadAll(io.LimitReader(part, 64))
				_ = part.Close()
				if rerr != nil {
					s.send(w, "fail", "multipart read failed", http.StatusBadRequest)
					return
				}
				v, verr := appmeta.ParseAgentVariant(string(b))
				if verr != nil {
					s.send(w, "fail", verr.Error(), http.StatusBadRequest)
					return
				}
				agentVariant = v
			case "reuse_previous_config":
				b, rerr := io.ReadAll(io.LimitReader(part, 16))
				_ = part.Close()
				if rerr != nil {
					s.send(w, "fail", "multipart read failed", http.StatusBadRequest)
					return
				}
				reusePreviousConfig = parseReusePreviousConfig(string(b))
			default:
				_, _ = io.Copy(io.Discard, part)
				part.Close()
			}
		}
		ip := remoteIP
		if ip == "" || ip == "self" {
			s.send(w, "fail", "ip is required for remote apply", http.StatusBadRequest)
			return
		}
		if len(bundleData) == 0 {
			s.send(w, "fail", "bundle file required (multipart field \""+uploadBundleField+"\", tar.gz)", http.StatusBadRequest)
			return
		}

		pb, err := prepareAgentBundle(base, bytes.NewReader(bundleData), s.maxUploadBytes)
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = os.RemoveAll(pb.WorkDir) }()

		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote apply failed: "+err.Error(), http.StatusOK)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 280*time.Second)
		defer cancel()
		var uploadErr error
		if reusePreviousConfig {
			content, err := s.fetchRemoteCurrentConfigContent(ctx, baseURL, s.apiPrefix)
			if err != nil {
				s.send(w, "fail", "reuse remote config: "+err.Error(), http.StatusOK)
				return
			}
			pb.ConfigData = []byte(content)
			uploadDir := filepath.Join(pb.WorkDir, "remote-staging")
			_ = os.RemoveAll(uploadDir)
			if err := os.MkdirAll(uploadDir, 0755); err != nil {
				s.send(w, "fail", "remote staging dir: "+err.Error(), http.StatusOK)
				return
			}
			if err := StagePreparedBundle(uploadDir, pb); err != nil {
				s.send(w, "fail", err.Error(), http.StatusOK)
				return
			}
			uploadErr = s.postUploadToTarget(ctx, baseURL, s.apiPrefix, uploadDir)
		} else {
			uploadErr = s.postUploadBundlePath(ctx, baseURL, s.apiPrefix, pb.BundlePath)
		}
		if uploadErr != nil {
			s.send(w, "fail", uploadErr.Error(), http.StatusOK)
			return
		}
		status, data, err := s.postApplyUpdateToTarget(ctx, baseURL, s.apiPrefix, pb.VersionKey, agentVariant, reusePreviousConfig)
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusOK)
			return
		}
		if status != "success" {
			msg := "remote apply failed"
			if msgStr, ok := data.(string); ok && msgStr != "" {
				msg = msgStr
			}
			s.send(w, "fail", msg, http.StatusOK)
			return
		}
		log.Printf("apply-update: remote %s version %s applied (multipart -> upload API, agent_variant=%s, reuse_previous_config=%v)", ip, pb.VersionKey, agentVariant, reusePreviousConfig)
		s.send(w, "success", "version "+pb.VersionKey+" applied on remote "+ip+". Refresh service status.", http.StatusOK)
		return
	}

	var req struct {
		Version             string `json:"version"`
		IP                  string `json:"ip"`
		AgentVariant        string `json:"agent_variant"`
		ReusePreviousConfig *bool  `json:"reuse_previous_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		s.send(w, "fail", "version is required", http.StatusBadRequest)
		return
	}
	if err := agentcfg.ValidateVersionKeyPath(version); err != nil {
		s.send(w, "fail", "version contains invalid characters", http.StatusBadRequest)
		return
	}

	agentVariant, err := appmeta.ParseAgentVariant(req.AgentVariant)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusBadRequest)
		return
	}

	versionDir, _ := s.resolveVersionDir(base, version)
	if versionDir == "" {
		s.send(w, "fail", "version not found in staging or versions/: "+version, http.StatusOK)
		return
	}

	reusePreviousConfig := false
	if req.ReusePreviousConfig != nil {
		reusePreviousConfig = *req.ReusePreviousConfig
	}

	ip := strings.TrimSpace(req.IP)
	if ip == "" || ip == "self" {
		if err := s.runUpdateViaEmbeddedScript(base, version, agentVariant, reusePreviousConfig); err != nil {
			s.send(w, "fail", err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", "Update in progress; the service will restart shortly. Refresh the update log below.", http.StatusOK)
		return
	}

	s.doRemoteUpdate(w, ip, version, versionDir, agentVariant, reusePreviousConfig)
}

// doRemoteUpdate sends files to the remote upload API (staging), then calls the remote apply-update API (no SSH/SCP).
func (s *Server) doRemoteUpdate(w http.ResponseWriter, ip, version, versionDir, agentVariant string, reusePreviousConfig bool) {
	if err := s.applyUpdateOnRemote(ip, version, versionDir, agentVariant, reusePreviousConfig); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	log.Printf("apply-update: remote %s version %s applied (upload API, agent_variant=%s, reuse_previous_config=%v)", ip, version, agentVariant, reusePreviousConfig)
	s.send(w, "success", "version "+version+" applied on remote "+ip+". Refresh service status.", http.StatusOK)
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	base := s.deployBaseOrDefault()
	// Symlink target name under versions/ (EvalSymlinks + Rel); may differ from running process if link moved before restart.
	symlinkVersion := strings.TrimSpace(s.resolveSymlinkVersion(base, "current"))

	stagingVersions := s.localStagingVersions()
	stagingParent := filepath.Join(base, "staging")

	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	var compareKey string
	if ip != "" && ip != "self" {
		rv, err := s.fetchRemoteVersionKey(ip)
		if err != nil {
			s.send(w, "fail", "remote version query failed: "+err.Error(), http.StatusOK)
			return
		}
		s.remoteRegistry.UpsertFromRemoteIP(ip)
		compareKey = strings.TrimSpace(rv)
	} else {
		// Local: compare against the running agent (same as GET /self / GET /version), not only the current symlink.
		// Otherwise symlink can already point at staging/versions key N while the process is still N-1 → can_apply stays false.
		compareKey = strings.TrimSpace(s.version)
		if compareKey == "" {
			compareKey = symlinkVersion
		}
	}

	var applyVersion, removeVersion string
	canApply := false
	for _, v := range stagingVersions {
		if agentcfg.StagingUpdateAvailable(v, compareKey, s.allowSameVersionUpdate) {
			canApply = true
			if applyVersion == "" {
				applyVersion = v
			}
		}
	}
	if len(stagingVersions) > 0 {
		removeVersion = stagingVersions[len(stagingVersions)-1]
	}
	stagingDualAgents := false
	for _, v := range stagingVersions {
		if versionsapi.StagingHasDualAgents(filepath.Join(stagingParent, v)) {
			stagingDualAgents = true
			break
		}
	}
	out := map[string]interface{}{
		"staging_versions":     stagingVersions,
		"can_apply":            canApply,
		"apply_version":        applyVersion,
		"remove_version":       removeVersion,
		"update_in_progress":   isUpdateUnitActive(),
		"staging_dual_agents":  stagingDualAgents,
		"default_agent_variant": appmeta.AgentVariantCompute,
	}
	if ip != "" && ip != "self" {
		out["remote_ip"] = ip
		out["remote_current_version"] = compareKey
	} else {
		out["current_version"] = compareKey
	}
	s.send(w, "success", out, http.StatusOK)
}

// isUpdateUnitActive returns true while the transient update unit (UpdateTransientUnit) is active.
func isUpdateUnitActive() bool {
	out, err := exec.Command("systemctl", "is-active", appmeta.UpdateTransientUnit).Output()
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

func (s *Server) handleVersionsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip != "" && ip != "self" {
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote versions list request failed: "+err.Error(), http.StatusOK)
			return
		}
		baseURL = baseURL + s.apiPrefix + "/versions/list"
		resp, err := remoteHTTPClient.Get(baseURL)
		if err != nil {
			s.send(w, "fail", "remote versions list request failed: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "failed to parse remote response", http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}
	base := s.versionsBase()
	list, err := versionsapi.ListInstalledVersions(base)
	if err != nil {
		s.send(w, "fail", "cannot read versions directory: "+err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", map[string]interface{}{"versions": list}, http.StatusOK)
}

// resolveSymlinkVersion returns the version name (dir under base/versions/) that the symlink base/name points to, or "".
func (s *Server) resolveSymlinkVersion(base, name string) string {
	return versionsapi.ResolveSymlinkVersion(base, name)
}

func (s *Server) handleVersionsRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Versions []string `json:"versions"`
		IP       string   `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(req.IP)
	if ip != "" && ip != "self" {
		// 실제 삭제·버전 검증은 ip로 지정된 호스트의 에이전트에서 수행된다. 그쪽 바이너리를 갱신해야 한다.
		for _, ver := range req.Versions {
			ver = strings.TrimSpace(ver)
			if ver == "" {
				continue
			}
			if err := agentcfg.ValidateVersionKeyPath(ver); err != nil {
				s.send(w, "fail", ver+": "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote versions remove request failed: "+err.Error(), http.StatusOK)
			return
		}
		baseURL = baseURL + s.apiPrefix + "/versions/remove"
		payload, _ := json.Marshal(map[string]interface{}{"versions": req.Versions})
		hr, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
		hr.Header.Set("Content-Type", "application/json")
		resp, err := remoteHTTPClient.Do(hr)
		if err != nil {
			s.send(w, "fail", "remote versions remove request failed: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "failed to parse remote response", http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}
	base := s.versionsBase()
	currentVer := s.resolveSymlinkVersion(base, "current")
	previousVer := s.resolveSymlinkVersion(base, "previous")
	var removed []string
	var skipped []string
	versionsParent := filepath.Join(base, "versions")
	for _, ver := range req.Versions {
		ver = strings.TrimSpace(ver)
		if ver == "" {
			continue
		}
		if err := agentcfg.ValidateVersionKeyPath(ver); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s (%v)", ver, err))
			continue
		}
		if ver == currentVer {
			skipped = append(skipped, ver+" (current)")
			continue
		}
		if ver == previousVer {
			skipped = append(skipped, ver+" (previous, for rollback)")
			continue
		}
		dir := s.versionsDir(base, ver)
		clean := filepath.Clean(dir)
		rel, relErr := filepath.Rel(versionsParent, clean)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, "..") || clean == versionsParent {
			skipped = append(skipped, ver+" (invalid path)")
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			skipped = append(skipped, ver+": "+err.Error())
			continue
		}
		removed = append(removed, ver)
	}
	if len(removed) > 0 {
		log.Printf("versions/remove: deleted %v from %s/versions", removed, base)
	}
	msg := ""
	if len(removed) > 0 {
		msg = "removed: " + strings.Join(removed, ", ")
	}
	if len(skipped) > 0 {
		if msg != "" {
			msg += ". "
		}
		msg += "skipped: " + strings.Join(skipped, "; ")
	}
	if msg == "" {
		msg = "no versions selected for removal."
	}
	s.send(w, "success", msg, http.StatusOK)
}

// handleVersionsSwitchCurrent POST body: { "version": "<키>", "ip": "" | "self" | "<원격>" } — 지정 버전을 current로 두기 위해
// 내장 update.sh를 systemd-run으로 실행한다(apply-update 로컬 경로와 동일). 원격 ip면 해당 호스트 API로 프록시.
func (s *Server) handleVersionsSwitchCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Version string `json:"version"`
		IP      string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		s.send(w, "fail", "version is required", http.StatusBadRequest)
		return
	}
	if err := agentcfg.ValidateVersionKeyPath(version); err != nil {
		s.send(w, "fail", "version contains invalid characters", http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(req.IP)
	if ip != "" && ip != "self" {
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote request failed: "+err.Error(), http.StatusOK)
			return
		}
		u := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(s.apiPrefix, "/") + "/versions/switch-current"
		payload, err := json.Marshal(map[string]string{"version": version})
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusInternalServerError)
			return
		}
		hr, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusInternalServerError)
			return
		}
		hr.Header.Set("Content-Type", "application/json")
		resp, err := remoteHTTPClient.Do(hr)
		if err != nil {
			s.send(w, "fail", "remote switch-current request failed: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "failed to parse remote response", http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}

	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	if dir, _ := s.resolveVersionDir(base, version); dir == "" {
		s.send(w, "fail", "version not found in staging or versions/: "+version, http.StatusOK)
		return
	}
	if err := s.runUpdateViaEmbeddedScript(base, version, "", false); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", "systemd-run started update.sh. Service restart and health checks may take tens of seconds. On failure check update_history.log and journal.", http.StatusOK)
}

func (s *Server) handleUpdateLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip != "" && ip != "self" {
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote update-log request failed: "+err.Error(), http.StatusOK)
			return
		}
		url := baseURL + s.apiPrefix + "/update-log"
		resp, err := remoteHTTPClient.Get(url)
		if err != nil {
			s.send(w, "fail", "remote update-log request failed: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "failed to parse remote response", http.StatusOK)
			return
		}
		if out.Status == "success" {
			payload := normalizeUpdateLogAPIPayload(out.Data, false)
			w.Header().Set("Cache-Control", "no-store")
			s.send(w, "success", payload, http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}
	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	historyPath := filepath.Join(base, "update_history.log")
	payload, err := updateLogPayloadFromFile(historyPath, isUpdateUnitActive())
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Cache-Control", "no-store")
			s.send(w, "success", map[string]interface{}{"output": "(no entries yet)", "recent_rollback": false}, http.StatusOK)
			return
		}
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.send(w, "success", payload, http.StatusOK)
}

const updateLogMaxLines = 10

func updateLogPayloadFromFile(historyPath string, updateInProgress bool) (map[string]interface{}, error) {
	data, err := os.ReadFile(historyPath)
	if err != nil {
		return nil, err
	}
	return normalizeUpdateLogAPIPayload(string(data), updateInProgress), nil
}

// normalizeUpdateLogAPIPayload returns tail lines oldest-first for the UI to reverse (newest on top).
// data may be raw file bytes/string or a proxied API map with an "output" field.
func normalizeUpdateLogAPIPayload(data interface{}, updateInProgress bool) map[string]interface{} {
	raw := ""
	recentRollback := false
	switch v := data.(type) {
	case string:
		raw = v
	case map[string]interface{}:
		if o, ok := v["output"].(string); ok {
			raw = o
		}
		if rb, ok := v["recent_rollback"].(bool); ok {
			recentRollback = rb
		}
	case []byte:
		raw = string(v)
	}
	lines := splitUpdateLogLines(raw)
	if len(lines) == 0 {
		return map[string]interface{}{"output": "(no entries yet)", "recent_rollback": false}
	}
	outLines := lines
	if len(lines) > updateLogMaxLines {
		outLines = lines[len(lines)-updateLogMaxLines:]
	}
	output := strings.Join(outLines, "\n")
	if len(lines) > 0 {
		recentRollback = historyLineIndicatesRecentRollback(lines[len(lines)-1])
	}
	if recentRollback && updateInProgress {
		recentRollback = false
	}
	return map[string]interface{}{"output": output, "recent_rollback": recentRollback}
}

func splitUpdateLogLines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "(no entries yet)" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// currentConfigPath returns the path to deploy_base/current/<config file> (current symlink resolved), or "" if not available.
func (s *Server) currentConfigPath() string {
	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	linkPath := filepath.Join(base, "current")
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return ""
	}
	return filepath.Join(resolved, appmeta.ConfigFileName)
}

func (s *Server) handleCurrentConfig(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	var postContent string
	var backupBeforeWrite bool
	if r.Method == http.MethodPost {
		var reqBody struct {
			IP                string `json:"ip"`
			Content           string `json:"content"`
			BackupBeforeWrite bool   `json:"backup_before_write"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			s.send(w, "fail", "invalid body", http.StatusBadRequest)
			return
		}
		postContent = reqBody.Content
		backupBeforeWrite = reqBody.BackupBeforeWrite
		if strings.TrimSpace(reqBody.IP) != "" {
			ip = strings.TrimSpace(reqBody.IP)
		}
	}
	if ip != "" && ip != "self" {
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote config request failed: "+err.Error(), http.StatusOK)
			return
		}
		baseURL = baseURL + s.apiPrefix + "/current-config"
		if r.Method == http.MethodGet {
			resp, err := remoteHTTPClient.Get(baseURL)
			if err != nil {
				s.send(w, "fail", "remote config request failed: "+err.Error(), http.StatusOK)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var out APIResponse
			if json.Unmarshal(body, &out) != nil {
				s.send(w, "fail", "failed to parse remote response", http.StatusOK)
				return
			}
			s.send(w, out.Status, out.Data, http.StatusOK)
			return
		}
		if r.Method == http.MethodPost {
			payload, _ := json.Marshal(map[string]interface{}{
				"content":               postContent,
				"backup_before_write":   true,
			})
			req, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			resp, err := remoteHTTPClient.Do(req)
			if err != nil {
				s.send(w, "fail", "remote config save request failed: "+err.Error(), http.StatusOK)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var out APIResponse
			if json.Unmarshal(body, &out) != nil {
				s.send(w, "fail", "failed to parse remote response", http.StatusOK)
				return
			}
			s.send(w, out.Status, out.Data, http.StatusOK)
			return
		}
	}
	configPath := s.currentConfigPath()
	if configPath == "" {
		s.send(w, "fail", "current version symlink not found", http.StatusOK)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				s.send(w, "success", map[string]interface{}{"content": ""}, http.StatusOK)
				return
			}
			s.send(w, "fail", appmeta.ConfigFileName+" read failed: "+err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", map[string]interface{}{"content": string(data)}, http.StatusOK)
		return
	case http.MethodPost:
		if err := saveCurrentConfigContent(configPath, postContent, backupBeforeWrite); err != nil {
			s.send(w, "fail", err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", nil, http.StatusOK)
		return
	default:
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCurrentConfigPushLocal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(req.IP)
	if ip == "" || ip == "self" {
		s.send(w, "fail", "remote ip is required", http.StatusBadRequest)
		return
	}
	content, err := s.readLocalCurrentConfigContent()
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	if err := s.pushConfigContentToRemote(content, ip); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", map[string]string{
		"message": "local current " + appmeta.ConfigFileName + " pushed to " + ip,
	}, http.StatusOK)
}

func (s *Server) send(w http.ResponseWriter, status string, data interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(APIResponse{Status: status, Data: data})
}
