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
	"sort"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"contrabass-agent/internal/config"
	"contrabass-agent/internal/updatescripts"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfo"
	"contrabass-agent/maintenance/svcstatus"
)

// uploadBinaryField was the legacy multipart field for a single agent binary; retained for comments only.
// Upload now uses uploadBundleField (tar.gz); see bundleupload.go.
const uploadBinaryField = "agent"

func dirHasAgentBinary(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, appmeta.BinaryName))
	return err == nil
}

func firstAgentBinaryPath(dir string) string {
	p := filepath.Join(dir, appmeta.BinaryName)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// elfMagic is the first 4 bytes of an ELF executable.
var elfMagic = []byte{0x7f, 'E', 'L', 'F'}

func isELFExecutable(header []byte) bool {
	return len(header) >= 4 && header[0] == elfMagic[0] && header[1] == elfMagic[1] && header[2] == elfMagic[2] && header[3] == elfMagic[3]
}

// versionKeyFromAgentBinary runs binPath --version and returns the version key after "<BinaryName> " (same string as GET /version).
func versionKeyFromAgentBinary(binPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("실행 파일 검증 시간 초과 (--version이 5초 내에 끝나지 않음)")
		}
		return "", fmt.Errorf("실행 파일이 아닌 것 같습니다 (--version 실패): %w", err)
	}
	line := strings.TrimSpace(string(out))
	want := appmeta.BinaryName + " "
	if !strings.HasPrefix(line, want) {
		return "", fmt.Errorf("실행 파일이 아닌 것 같습니다 (--version 출력 접두사 기대 %q, 실제: %q)", want, line)
	}
	key := strings.TrimSpace(strings.TrimPrefix(line, want))
	if key == "" {
		return "", fmt.Errorf("실행 파일이 아닌 것 같습니다 (--version에 버전 키가 비어 있음)")
	}
	if err := config.ValidateVersionKeyPath(key); err != nil {
		return "", fmt.Errorf("실행 파일의 버전 키가 유효하지 않습니다: %w", err)
	}
	return key, nil
}

// validateAgentBinary runs binPath --version and checks output shape and version key.
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
	maxUploadBytes       int64 // POST /upload & multipart apply-update: max body (tar.gz bundle field)
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
	MaxUploadBytes       int    // 0 or omit → config.DefaultMaxUploadBytes (64<<20); max multipart body for upload / multipart apply-update
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
		maxUploadBytes:       normalizeMaxUploadBytes(cfg.MaxUploadBytes),
	}
	if s.installPrefix == "" {
		s.installPrefix = s.deployBase
	}
	return s
}

func normalizeMaxUploadBytes(n int) int64 {
	const minB = int64(1 << 20)  // 1 MiB
	const maxB = int64(10 << 30) // 10 GiB
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
	fmt.Fprintf(w, "window.__CONTRABASS_API_PREFIX__=%s;\n", quoted)
}

// Handler returns http.Handler that serves web and API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/", s.handleRoot)
	// API
	mux.HandleFunc(s.apiPrefix+"/self", s.handleSelf)
	mux.HandleFunc(s.apiPrefix+"/host-info", s.handleHostInfo)
	mux.HandleFunc(s.apiPrefix+"/discovery", s.handleDiscovery)
	mux.HandleFunc(s.apiPrefix+"/discovery/stream", s.handleDiscoveryStream)
	mux.HandleFunc(s.apiPrefix+"/service-status", s.handleServiceStatus)
	mux.HandleFunc(s.apiPrefix+"/service-control", s.handleServiceControl)
	mux.HandleFunc(s.apiPrefix+"/upload", s.handleUpload)
	mux.HandleFunc(s.apiPrefix+"/upload/remove", s.handleRemoveUpload)
	mux.HandleFunc(s.apiPrefix+"/update-status", s.handleUpdateStatus)
	mux.HandleFunc(s.apiPrefix+"/apply-update", s.handleApplyUpdate)
	mux.HandleFunc(s.apiPrefix+"/update-log", s.handleUpdateLog)
	mux.HandleFunc(s.apiPrefix+"/current-config", s.handleCurrentConfig)
	mux.HandleFunc(s.apiPrefix+"/versions/list", s.handleVersionsList)
	mux.HandleFunc(s.apiPrefix+"/versions/remove", s.handleVersionsRemove)
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
	data := discovery.DiscoveryResponse{
		Type:                "DISCOVERY_RESPONSE",
		Service:             s.discoveryServiceName,
		HostIP:              info.HostIP,
		HostIPs:             info.HostIPs,
		Hostname:            info.Hostname,
		ServicePort:         s.servicePort,
		Version:             s.version,
		RequestID:           "",
		CPUInfo:             info.CPUInfo,
		CPUUsagePercent:     info.CPUUsagePercent,
		CPUUUID:             info.CPUUUID,
		MemoryTotalMB:       info.MemoryTotalMB,
		MemoryUsedMB:        info.MemoryUsedMB,
		MemoryUsagePercent:  info.MemoryUsagePercent,
	}
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
	resp, err := s.discovery.DoDiscoveryUnicast(ip)
	if err != nil {
		log.Printf("discovery: ERROR: DoDiscoveryUnicast(ip=%s) failed: %v", ip, err)
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
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
			s.send(w, "fail", "원격 상태 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		url := baseURL + s.apiPrefix + "/service-status"
		resp, err := remoteHTTPClient.Get(url)
		if err != nil {
			s.send(w, "fail", "원격 상태 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "원격 응답 파싱 실패", http.StatusOK)
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
				s.send(w, "fail", "원격 재시작 요청 실패: "+err.Error(), http.StatusOK)
				return
			}
			baseURL = baseURL + s.apiPrefix + "/service-control"
			payload, _ := json.Marshal(map[string]string{"ip": "self", "action": "restart"})
			req, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			resp, err := remoteHTTPClient.Do(req)
			if err != nil {
				s.send(w, "fail", "원격 재시작 요청 실패: "+err.Error(), http.StatusOK)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var out APIResponse
			if json.Unmarshal(body, &out) != nil {
				s.send(w, "fail", "원격 응답 파싱 실패", http.StatusOK)
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
			s.send(w, "fail", "원격 SSH 제어 실패: "+err.Error(), http.StatusOK)
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
	base := s.installPrefix
	if base == "" {
		base = s.deployBase
	}
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	return base
}

// writeToStaging writes the agent binary from execReader and configData to base/staging/version/. Returns the staging dir path.
func (s *Server) writeToStaging(base, version string, execReader io.Reader, configData []byte) (string, error) {
	stagingDir := s.stagingDir(base, version)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", fmt.Errorf("스테이징 디렉터리 생성 실패: %w", err)
	}
	binName := appmeta.BinaryName
	binPath := filepath.Join(stagingDir, binName)
	configPath := filepath.Join(stagingDir, "config.yaml")
	binOut, err := os.Create(binPath)
	if err != nil {
		return "", fmt.Errorf("%s 파일 저장 실패: %w", binName, err)
	}
	_, err = io.Copy(binOut, execReader)
	binOut.Close()
	if err != nil {
		os.Remove(binPath)
		return "", fmt.Errorf("%s 쓰기 실패: %w", binName, err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		log.Printf("chmod %s: %v", binPath, err)
	}
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		os.Remove(binPath)
		return "", fmt.Errorf("config.yaml 저장 실패: %w", err)
	}
	return stagingDir, nil
}

// copyStagingToVersions replaces base/versions/version/ with a full copy of base/staging/version/
// (every extracted file and subdirectory), then removes only StagedBundleFileName so versions/ holds
// the installed tree without the original tar.gz. Future manifest-added files are included automatically.
func (s *Server) copyStagingToVersions(base, version string) error {
	stg := s.stagingDir(base, version)
	ver := s.versionsDir(base, version)
	if _, err := os.Stat(stg); err != nil {
		return fmt.Errorf("스테이징 디렉터리: %w", err)
	}
	if err := os.RemoveAll(ver); err != nil {
		return err
	}
	if err := os.MkdirAll(ver, 0755); err != nil {
		return err
	}
	if err := copyStagingTreeInto(stg, ver); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(ver, StagedBundleFileName))
	return nil
}

// copyStagingTreeInto recursively copies files and directories from stg to ver (both must exist; ver is empty).
func copyStagingTreeInto(stg, ver string) error {
	return filepath.WalkDir(stg, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(stg, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(ver, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0644
		}
		return copyFileRobust(path, dst, mode)
	})
}

func copyFileRobust(src, dst string, perm fs.FileMode) error {
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

// resolveVersionDir returns the directory that contains the agent binary + config for this version: staging first, then versions.
func (s *Server) resolveVersionDir(base, version string) (string, bool) {
	stg := s.stagingDir(base, version)
	if dirHasAgentBinary(stg) {
		return stg, true // from staging
	}
	ver := s.versionsDir(base, version)
	if dirHasAgentBinary(ver) {
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
		s.send(w, "fail", "요청이 multipart가 아니거나 본문을 읽을 수 없습니다", http.StatusBadRequest)
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
			s.send(w, "fail", "요청 크기 초과이거나 multipart 읽기 실패", http.StatusBadRequest)
			return
		}
		switch part.FormName() {
		case uploadBundleField:
			buf := new(bytes.Buffer)
			_, err := io.Copy(buf, io.LimitReader(part, s.maxUploadBytes))
			_ = part.Close()
			if err != nil {
				s.send(w, "fail", "번들 파트 읽기 실패: "+err.Error(), http.StatusBadRequest)
				return
			}
			bundleData = buf.Bytes()
		default:
			_, _ = io.Copy(io.Discard, part)
			part.Close()
		}
	}
	if len(bundleData) == 0 {
		s.send(w, "fail", "번들 파일이 필요합니다 (multipart 필드 \""+uploadBundleField+"\", tar.gz)", http.StatusBadRequest)
		return
	}

	versionKey, configData, _, workDir, agentSrc, err := prepareAgentBundle(base, bytes.NewReader(bundleData), s.maxUploadBytes)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	s.clearStaging(base)

	finalDir := s.stagingDir(base, versionKey)
	if err := os.MkdirAll(filepath.Join(base, "staging"), 0755); err != nil {
		s.send(w, "fail", "스테이징 디렉터리 생성 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		s.send(w, "fail", "스테이징 버전 디렉터리 생성 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}

	binDst := filepath.Join(finalDir, appmeta.BinaryName)
	srcf, err := os.Open(agentSrc)
	if err != nil {
		s.send(w, "fail", "실행 파일 읽기 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}
	dstf, err := os.OpenFile(binDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		_ = srcf.Close()
		s.send(w, "fail", "스테이징 실행 파일 쓰기 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(dstf, srcf)
	_ = srcf.Close()
	_ = dstf.Close()
	if err != nil {
		_ = os.RemoveAll(finalDir)
		s.send(w, "fail", "실행 파일 복사 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(filepath.Join(finalDir, "config.yaml"), configData, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		s.send(w, "fail", "config.yaml 저장 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := validateAgentBinary(binDst); err != nil {
		_ = os.RemoveAll(finalDir)
		s.send(w, "fail", err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.WriteFile(filepath.Join(finalDir, StagedBundleFileName), bundleData, 0644); err != nil {
		_ = os.RemoveAll(finalDir)
		s.send(w, "fail", "원본 번들 저장 실패: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("upload: version %s -> %s (staging)", versionKey, finalDir)
	s.send(w, "success", map[string]string{"version": versionKey}, http.StatusOK)
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
		s.send(w, "fail", "version이 필요합니다", http.StatusBadRequest)
		return
	}
	if err := config.ValidateVersionKeyPath(version); err != nil {
		s.send(w, "fail", "version에 허용되지 않은 문자가 있습니다", http.StatusBadRequest)
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
		s.send(w, "fail", "잘못된 버전 경로입니다", http.StatusBadRequest)
		return
	}
	if err := os.RemoveAll(stagingVersionDir); err != nil {
		s.send(w, "fail", "삭제 실패: "+err.Error(), http.StatusOK)
		return
	}
	log.Printf("upload/remove: version %s removed from staging %s", version, stagingVersionDir)
	s.send(w, "success", "버전 "+version+" 이 스테이징에서 삭제되었습니다.", http.StatusOK)
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
	binPath := filepath.Join(versionDir, appmeta.BinaryName)
	configPath := filepath.Join(versionDir, "config.yaml")
	tmp, err := os.CreateTemp("", "remote-bundle-*.tar.gz")
	if err != nil {
		return fmt.Errorf("임시 번들: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := writeBundleTarGz(tmp, binPath, configPath); err != nil {
		_ = tmp.Close()
		return err
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
		return fmt.Errorf("번들 읽기: %w", err)
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
		return fmt.Errorf("원격 업로드 요청: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Status string      `json:"status"`
		Data   interface{} `json:"data"`
	}
	_ = json.Unmarshal(body, &out)
	if out.Status != "success" {
		msg := "원격 업로드 실패"
		if s, ok := out.Data.(string); ok && s != "" {
			msg = s
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// postApplyUpdateToTarget tells the target agent to apply the given version from its staging (ip=self).
func (s *Server) postApplyUpdateToTarget(ctx context.Context, baseURL, apiPrefix, version string) (status string, data interface{}, err error) {
	applyURL := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(apiPrefix, "/") + "/apply-update"
	payload, err := json.Marshal(map[string]string{"version": version, "ip": "self"})
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
		return "", nil, fmt.Errorf("원격 적용 요청: %w", err)
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
			s.send(w, "fail", "multipart 파싱 실패", http.StatusBadRequest)
			return
		}
		var remoteIP string
		var bundleData []byte
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				s.send(w, "fail", "요청 크기 초과이거나 multipart 읽기 실패", http.StatusBadRequest)
				return
			}
			switch part.FormName() {
			case "ip":
				b, rerr := io.ReadAll(io.LimitReader(part, 256))
				if rerr != nil {
					part.Close()
					s.send(w, "fail", "multipart 읽기 실패", http.StatusBadRequest)
					return
				}
				_ = part.Close()
				remoteIP = strings.TrimSpace(string(b))
			case uploadBundleField:
				buf := new(bytes.Buffer)
				_, err := io.Copy(buf, io.LimitReader(part, s.maxUploadBytes))
				_ = part.Close()
				if err != nil {
					s.send(w, "fail", "번들 파트 읽기 실패: "+err.Error(), http.StatusBadRequest)
					return
				}
				bundleData = buf.Bytes()
			default:
				_, _ = io.Copy(io.Discard, part)
				part.Close()
			}
		}
		ip := remoteIP
		if ip == "" || ip == "self" {
			s.send(w, "fail", "원격 적용 시 ip가 필요합니다", http.StatusBadRequest)
			return
		}
		if len(bundleData) == 0 {
			s.send(w, "fail", "번들 파일이 필요합니다 (multipart 필드 \""+uploadBundleField+"\", tar.gz)", http.StatusBadRequest)
			return
		}

		versionKey, _, bundlePath, workDir, _, err := prepareAgentBundle(base, bytes.NewReader(bundleData), s.maxUploadBytes)
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = os.RemoveAll(workDir) }()

		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "원격 적용 실패: "+err.Error(), http.StatusOK)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 280*time.Second)
		defer cancel()
		if err := s.postUploadBundlePath(ctx, baseURL, s.apiPrefix, bundlePath); err != nil {
			s.send(w, "fail", err.Error(), http.StatusOK)
			return
		}
		status, data, err := s.postApplyUpdateToTarget(ctx, baseURL, s.apiPrefix, versionKey)
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusOK)
			return
		}
		if status != "success" {
			msg := "원격 적용 실패"
			if msgStr, ok := data.(string); ok && msgStr != "" {
				msg = msgStr
			}
			s.send(w, "fail", msg, http.StatusOK)
			return
		}
		log.Printf("apply-update: remote %s version %s applied (multipart -> upload API)", ip, versionKey)
		s.send(w, "success", "원격 "+ip+" 에 버전 "+versionKey+" 적용 완료. 서비스 상태를 새로고침하세요.", http.StatusOK)
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
		s.send(w, "fail", "version이 필요합니다", http.StatusBadRequest)
		return
	}
	if err := config.ValidateVersionKeyPath(version); err != nil {
		s.send(w, "fail", "version에 허용되지 않은 문자가 있습니다", http.StatusBadRequest)
		return
	}

	versionDir, fromStaging := s.resolveVersionDir(base, version)
	if versionDir == "" {
		s.send(w, "fail", "해당 버전이 스테이징 또는 versions에 없습니다: "+version, http.StatusOK)
		return
	}

	ip := strings.TrimSpace(req.IP)
	if ip == "" || ip == "self" {
		// Local update: if only in staging, copy to versions then run update.sh; after success remove staging
		if fromStaging {
			if err := s.copyStagingToVersions(base, version); err != nil {
				s.send(w, "fail", "스테이징→versions 복사 실패: "+err.Error(), http.StatusOK)
				return
			}
		}
		currentPath := filepath.Join(base, "current")
		if _, err := os.Stat(currentPath); err != nil {
			s.send(w, "fail", "배포 루트에 current가 없습니다. 업데이트를 적용할 수 없습니다: "+currentPath, http.StatusOK)
			return
		}
		updateScript := filepath.Join(currentPath, "update.sh")
		rollbackScript := filepath.Join(currentPath, "rollback.sh")
		if err := os.WriteFile(updateScript, []byte(updatescripts.UpdateSh), 0755); err != nil {
			s.send(w, "fail", "update.sh 쓰기 실패: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(rollbackScript, []byte(updatescripts.RollbackSh), 0755); err != nil {
			_ = os.Remove(updateScript)
			s.send(w, "fail", "rollback.sh 쓰기 실패: "+err.Error(), http.StatusInternalServerError)
			return
		}
		exec.Command("systemctl", "reset-failed", appmeta.UpdateTransientUnit).Run()
		exec.Command("systemctl", "stop", appmeta.UpdateTransientUnit).Run()
		go func() {
			cmd := exec.Command("systemd-run",
				"--unit="+appmeta.UpdateTransientUnitStem,
				"--property=RemainAfterExit=yes",
				"/bin/bash", updateScript, version)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			if err := cmd.Run(); err != nil {
				log.Printf("apply-update: systemd-run failed: %v", err)
				_ = os.Remove(updateScript)
				_ = os.Remove(rollbackScript)
				return
			}
			// 스테이징은 자동 삭제하지 않음. 원격 업데이트에 재사용할 수 있도록 남겨 두고, 사용자가 「업로드된 버전 삭제」로 수동 삭제.
			// update.sh / rollback.sh 는 스크립트 종료 시 스스로 삭제한다.
		}()
		log.Printf("apply-update: systemd-run --unit=%s /bin/bash %s %s", appmeta.UpdateTransientUnitStem, updateScript, version)
		s.send(w, "success", "업데이트를 적용 중입니다. 잠시 후 서버가 재시작됩니다. 아래 로그를 새로고침하세요.", http.StatusOK)
		return
	}

	s.doRemoteUpdate(w, ip, version, versionDir)
}

// doRemoteUpdate sends files to the remote upload API (staging), then calls the remote apply-update API (no SSH/SCP).
func (s *Server) doRemoteUpdate(w http.ResponseWriter, ip, version, versionDir string) {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		s.send(w, "fail", "원격 적용 실패: "+err.Error(), http.StatusOK)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
	defer cancel()

	if firstAgentBinaryPath(versionDir) == "" {
		s.send(w, "fail", "버전 디렉터리에 실행 파일 "+appmeta.BinaryName+" 이 없습니다: "+versionDir, http.StatusOK)
		return
	}
	if err := s.postUploadToTarget(ctx, baseURL, s.apiPrefix, versionDir); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	status, data, err := s.postApplyUpdateToTarget(ctx, baseURL, s.apiPrefix, version)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	if status != "success" {
		msg := "원격 적용 실패"
		if msgStr, ok := data.(string); ok && msgStr != "" {
			msg = msgStr
		}
		s.send(w, "fail", msg, http.StatusOK)
		return
	}
	log.Printf("apply-update: remote %s version %s applied (upload API)", ip, version)
	s.send(w, "success", "원격 "+ip+" 에 버전 "+version+" 적용 완료. 서비스 상태를 새로고침하세요.", http.StatusOK)
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	base := s.deployBase
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	// Symlink target name under versions/ (EvalSymlinks + Rel); may differ from running process if link moved before restart.
	symlinkVersion := strings.TrimSpace(s.resolveSymlinkVersion(base, "current"))

	stagingParent := filepath.Join(base, "staging")
	stagingVersions := []string{}
	if entries, err := os.ReadDir(stagingParent); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			v := e.Name()
			if dirHasAgentBinary(filepath.Join(stagingParent, v)) {
				stagingVersions = append(stagingVersions, v)
			}
		}
	}
	sort.Slice(stagingVersions, func(i, j int) bool {
		return config.CompareVersionKeys(stagingVersions[i], stagingVersions[j]) > 0
	})

	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	var compareKey string
	if ip != "" && ip != "self" {
		rv, err := s.fetchRemoteVersionKey(ip)
		if err != nil {
			s.send(w, "fail", "원격 버전 조회 실패: "+err.Error(), http.StatusOK)
			return
		}
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
		if config.StagingUpdateAvailable(v, compareKey) {
			canApply = true
			if applyVersion == "" {
				applyVersion = v
			}
		}
	}
	if len(stagingVersions) > 0 {
		removeVersion = stagingVersions[len(stagingVersions)-1]
	}
	out := map[string]interface{}{
		"staging_versions":   stagingVersions,
		"can_apply":          canApply,
		"apply_version":      applyVersion,
		"remove_version":     removeVersion,
		"update_in_progress": isUpdateUnitActive(),
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

// versionEntry is one item in GET /api/v1/versions/list response.
type versionEntry struct {
	Version    string `json:"version"`
	IsCurrent  bool   `json:"is_current"`
	IsPrevious bool   `json:"is_previous"`
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
			s.send(w, "fail", "원격 versions 목록 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		baseURL = baseURL + s.apiPrefix + "/versions/list"
		resp, err := remoteHTTPClient.Get(baseURL)
		if err != nil {
			s.send(w, "fail", "원격 versions 목록 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "원격 응답 파싱 실패", http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}
	base := s.versionsBase()
	versionsParent := filepath.Join(base, "versions")
	entries, err := os.ReadDir(versionsParent)
	if err != nil {
		if os.IsNotExist(err) {
			s.send(w, "success", map[string]interface{}{"versions": []versionEntry{}}, http.StatusOK)
			return
		}
		s.send(w, "fail", "versions 디렉터리를 읽을 수 없습니다: "+err.Error(), http.StatusOK)
		return
	}
	currentVer := s.resolveSymlinkVersion(base, "current")
	previousVer := s.resolveSymlinkVersion(base, "previous")
	var list []versionEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ver := e.Name()
		if ver == "." || ver == ".." {
			continue
		}
		if !dirHasAgentBinary(filepath.Join(versionsParent, ver)) {
			continue
		}
		list = append(list, versionEntry{
			Version:    ver,
			IsCurrent:  ver == currentVer,
			IsPrevious: ver == previousVer,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return versionsListEntryBefore(list[i], list[j])
	})
	s.send(w, "success", map[string]interface{}{"versions": list}, http.StatusOK)
}

// versionsListEntryBefore defines display order: current → previous → others by semver descending (newest first).
func versionsListEntryBefore(a, b versionEntry) bool {
	rank := func(e versionEntry) int {
		if e.IsCurrent {
			return 2
		}
		if e.IsPrevious {
			return 1
		}
		return 0
	}
	ra, rb := rank(a), rank(b)
	if ra != rb {
		return ra > rb
	}
	return config.CompareVersionKeys(a.Version, b.Version) > 0
}

// resolveSymlinkVersion returns the version name (dir under base/versions/) that the symlink base/name points to, or "".
func (s *Server) resolveSymlinkVersion(base, name string) string {
	linkPath := filepath.Join(base, name)
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return ""
	}
	versionsDir := filepath.Join(base, "versions")
	rel, err := filepath.Rel(versionsDir, resolved)
	if err != nil {
		return ""
	}
	// rel should be like "0.3.0" or "0.3.0/something" — we want the top-level version dir only
	if rel == ".." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 1 && parts[0] != "" {
		return parts[0]
	}
	return ""
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
			if err := config.ValidateVersionKeyPath(ver); err != nil {
				s.send(w, "fail", ver+": "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "원격 버전 삭제 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		baseURL = baseURL + s.apiPrefix + "/versions/remove"
		payload, _ := json.Marshal(map[string]interface{}{"versions": req.Versions})
		hr, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
		hr.Header.Set("Content-Type", "application/json")
		resp, err := remoteHTTPClient.Do(hr)
		if err != nil {
			s.send(w, "fail", "원격 버전 삭제 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "원격 응답 파싱 실패", http.StatusOK)
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
		if err := config.ValidateVersionKeyPath(ver); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s (%v)", ver, err))
			continue
		}
		if ver == currentVer {
			skipped = append(skipped, ver+" (현재 실행 중)")
			continue
		}
		if ver == previousVer {
			skipped = append(skipped, ver+" (이전 버전, 롤백용)")
			continue
		}
		dir := s.versionsDir(base, ver)
		clean := filepath.Clean(dir)
		rel, relErr := filepath.Rel(versionsParent, clean)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, "..") || clean == versionsParent {
			skipped = append(skipped, ver+" (잘못된 경로)")
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
		msg = "삭제됨: " + strings.Join(removed, ", ")
	}
	if len(skipped) > 0 {
		if msg != "" {
			msg += ". "
		}
		msg += "제외: " + strings.Join(skipped, "; ")
	}
	if msg == "" {
		msg = "삭제할 버전을 선택하세요."
	}
	s.send(w, "success", msg, http.StatusOK)
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
			s.send(w, "fail", "원격 업데이트 로그 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		url := baseURL + s.apiPrefix + "/update-log"
		resp, err := remoteHTTPClient.Get(url)
		if err != nil {
			s.send(w, "fail", "원격 업데이트 로그 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var out APIResponse
		if json.Unmarshal(body, &out) != nil {
			s.send(w, "fail", "원격 응답 파싱 실패", http.StatusOK)
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
	data, err := os.ReadFile(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.send(w, "success", map[string]interface{}{"output": "(아직 기록 없음)", "recent_rollback": false}, http.StatusOK)
			return
		}
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	const maxLines = 5
	var outLines []string
	if len(lines) > maxLines {
		outLines = lines[:maxLines]
	} else {
		outLines = lines
	}
	output := strings.Join(outLines, "\n")
	if output == "" {
		output = "(아직 기록 없음)"
	}
	recentRollback := false
	if len(lines) > 0 {
		first := strings.ToLower(lines[0])
		recentRollback = strings.Contains(first, "rollback") || strings.Contains(first, "failed")
	}
	// 업데이트 진행 중에는 롤백 경고 숨김 (이전 실패 기록이 새 적용과 혼동되지 않도록)
	if recentRollback && isUpdateUnitActive() {
		recentRollback = false
	}
	s.send(w, "success", map[string]interface{}{"output": output, "recent_rollback": recentRollback}, http.StatusOK)
}

// currentConfigPath returns the path to deploy_base/current/config.yaml (current symlink resolved), or "" if not available.
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
	return filepath.Join(resolved, "config.yaml")
}

func (s *Server) handleCurrentConfig(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	var postContent string
	if r.Method == http.MethodPost {
		var reqBody struct {
			IP      string `json:"ip"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			s.send(w, "fail", "invalid body", http.StatusBadRequest)
			return
		}
		postContent = reqBody.Content
		if strings.TrimSpace(reqBody.IP) != "" {
			ip = strings.TrimSpace(reqBody.IP)
		}
	}
	if ip != "" && ip != "self" {
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "원격 config 요청 실패: "+err.Error(), http.StatusOK)
			return
		}
		baseURL = baseURL + s.apiPrefix + "/current-config"
		if r.Method == http.MethodGet {
			resp, err := remoteHTTPClient.Get(baseURL)
			if err != nil {
				s.send(w, "fail", "원격 config 요청 실패: "+err.Error(), http.StatusOK)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var out APIResponse
			if json.Unmarshal(body, &out) != nil {
				s.send(w, "fail", "원격 응답 파싱 실패", http.StatusOK)
				return
			}
			s.send(w, out.Status, out.Data, http.StatusOK)
			return
		}
		if r.Method == http.MethodPost {
			payload, _ := json.Marshal(map[string]string{"content": postContent})
			req, _ := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			resp, err := remoteHTTPClient.Do(req)
			if err != nil {
				s.send(w, "fail", "원격 config 저장 요청 실패: "+err.Error(), http.StatusOK)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			var out APIResponse
			if json.Unmarshal(body, &out) != nil {
				s.send(w, "fail", "원격 응답 파싱 실패", http.StatusOK)
				return
			}
			s.send(w, out.Status, out.Data, http.StatusOK)
			return
		}
	}
	configPath := s.currentConfigPath()
	if configPath == "" {
		s.send(w, "fail", "current 버전을 찾을 수 없습니다", http.StatusOK)
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
			s.send(w, "fail", "config.yaml 읽기 실패: "+err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", map[string]interface{}{"content": string(data)}, http.StatusOK)
		return
	case http.MethodPost:
		content := strings.TrimSpace(postContent)
		if content != "" {
			if _, err := config.LoadFromBytes([]byte(content)); err != nil {
				s.send(w, "fail", err.Error(), http.StatusOK)
				return
			}
		}
		if err := os.WriteFile(configPath, []byte(postContent), 0644); err != nil {
			s.send(w, "fail", "config.yaml 저장 실패: "+err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", nil, http.StatusOK)
		return
	default:
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
	}
}

func (s *Server) send(w http.ResponseWriter, status string, data interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(APIResponse{Status: status, Data: data})
}
