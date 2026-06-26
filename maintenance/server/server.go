package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
	"contrabass-agent/maintenance/hostinfoapi"
	"contrabass-agent/maintenance/server/remoteregistry"
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

func (s *Server) send(w http.ResponseWriter, status string, data interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(APIResponse{Status: status, Data: data})
}
