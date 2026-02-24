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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mol/config"
	"mol/discovery"
	"mol/hostinfo"
	"mol/svcstatus"
)

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
	serviceName          string
	systemctlServiceName string
	deployBase           string
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
	ServiceName          string
	SystemctlServiceName string
	DeployBase           string
}

// New creates a Server.
func New(cfg Config) *Server {
	return &Server{
		webPrefix:            strings.TrimSuffix(cfg.WebPrefix, "/"),
		apiPrefix:            strings.TrimSuffix(cfg.APIPrefix, "/"),
		webFS:                cfg.WebFS,
		discovery:            cfg.Discovery,
		getHostInfo:          cfg.GetHostInfo,
		version:              cfg.Version,
		servicePort:          cfg.ServicePort,
		serviceName:          cfg.ServiceName,
		systemctlServiceName: cfg.SystemctlServiceName,
		deployBase:           strings.TrimSuffix(cfg.DeployBase, "/"),
	}
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

// Handler returns http.Handler that serves web and API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
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
	// Web (static)
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
		Service:             s.serviceName,
		HostIP:              info.HostIP,
		Hostname:            info.Hostname,
		ServicePort:         s.servicePort,
		Version:             s.version,
		RequestID:           "",
		CPUInfo:             info.CPUInfo,
		CPUUsagePercent:     info.CPUUsagePercent,
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
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", resp, http.StatusOK)
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	list, err := s.discovery.DoDiscovery()
	if err != nil {
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
	ch, err := s.discovery.DoDiscoveryStream()
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusInternalServerError)
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
		svcName = "mol.service"
	}
	if ip != "" && ip != "self" {
		port := s.servicePort
		if port <= 0 {
			port = 8888
		}
		url := "http://" + ip + ":" + strconv.Itoa(port) + s.apiPrefix + "/service-status"
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
	Action string `json:"action"` // "start" or "stop"
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
	if action != "start" && action != "stop" {
		s.send(w, "fail", "action must be start or stop", http.StatusBadRequest)
		return
	}
	svcName := s.systemctlServiceName
	if svcName == "" {
		svcName = "mol.service"
	}
	if ip != "" && ip != "self" {
		port := s.servicePort
		if port <= 0 {
			port = 8888
		}
		url := "http://" + ip + ":" + strconv.Itoa(port) + s.apiPrefix + "/service-control"
		payload, _ := json.Marshal(map[string]string{"ip": "self", "action": action})
		resp, err := remoteHTTPClient.Post(url, "application/json", bytes.NewReader(payload))
		if err != nil {
			s.send(w, "fail", "원격 제어 요청 실패: "+err.Error(), http.StatusOK)
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
	var err error
	if action == "start" {
		err = svcstatus.StartLocal(svcName)
	} else {
		err = svcstatus.StopLocal(svcName)
	}
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", nil, http.StatusOK)
}

const maxUploadBytes = 64 << 20 // 64MB for mol binary + config

// stagingDir returns deploy_base/staging/<version>. Staging is never the running path, so no "text file busy".
func (s *Server) stagingDir(base, version string) string {
	return filepath.Join(base, "staging", version)
}

// clearStaging removes the entire deploy_base/staging/ directory so that upload replaces all staging content with the new version only.
func (s *Server) clearStaging(base string) {
	stagingParent := filepath.Join(base, "staging")
	_ = os.RemoveAll(stagingParent)
}

// versionsDir returns deploy_base/versions/<version> (the running path).
func (s *Server) versionsDir(base, version string) string {
	return filepath.Join(base, "versions", version)
}

// writeToStaging writes mol (from reader) and configData to base/staging/version/. Returns the staging dir path.
func (s *Server) writeToStaging(base, version string, molFile io.Reader, configData []byte) (string, error) {
	stagingDir := s.stagingDir(base, version)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", fmt.Errorf("스테이징 디렉터리 생성 실패: %w", err)
	}
	molPath := filepath.Join(stagingDir, "mol")
	configPath := filepath.Join(stagingDir, "config.yaml")
	molOut, err := os.Create(molPath)
	if err != nil {
		return "", fmt.Errorf("mol 파일 저장 실패: %w", err)
	}
	_, err = io.Copy(molOut, molFile)
	molOut.Close()
	if err != nil {
		os.Remove(molPath)
		return "", fmt.Errorf("mol 쓰기 실패: %w", err)
	}
	if err := os.Chmod(molPath, 0755); err != nil {
		log.Printf("chmod %s: %v", molPath, err)
	}
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		os.Remove(molPath)
		return "", fmt.Errorf("config.yaml 저장 실패: %w", err)
	}
	return stagingDir, nil
}

// copyStagingToVersions copies base/staging/version/ to base/versions/version/ (mol + config.yaml, chmod mol).
func (s *Server) copyStagingToVersions(base, version string) error {
	stg := s.stagingDir(base, version)
	ver := s.versionsDir(base, version)
	if err := os.MkdirAll(ver, 0755); err != nil {
		return err
	}
	molSrc := filepath.Join(stg, "mol")
	molDst := filepath.Join(ver, "mol")
	configSrc := filepath.Join(stg, "config.yaml")
	configDst := filepath.Join(ver, "config.yaml")
	data, err := os.ReadFile(molSrc)
	if err != nil {
		return err
	}
	if err := os.WriteFile(molDst, data, 0644); err != nil {
		return err
	}
	if err := os.Chmod(molDst, 0755); err != nil {
		os.Remove(molDst)
		return err
	}
	data, err = os.ReadFile(configSrc)
	if err != nil {
		os.Remove(molDst)
		return err
	}
	if err := os.WriteFile(configDst, data, 0644); err != nil {
		os.Remove(molDst)
		return err
	}
	return nil
}

// resolveVersionDir returns the directory that contains mol+config for this version: staging first, then versions.
func (s *Server) resolveVersionDir(base, version string) (string, bool) {
	stg := s.stagingDir(base, version)
	if _, err := os.Stat(filepath.Join(stg, "mol")); err == nil {
		return stg, true // from staging
	}
	ver := s.versionsDir(base, version)
	if _, err := os.Stat(filepath.Join(ver, "mol")); err == nil {
		return ver, false // from versions
	}
	return "", false
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.send(w, "fail", "요청 크기 초과 또는 multipart 파싱 실패", http.StatusBadRequest)
		return
	}
	molFile, _, err := r.FormFile("mol")
	if err != nil {
		s.send(w, "fail", "mol 파일이 필요합니다", http.StatusBadRequest)
		return
	}
	defer molFile.Close()
	configFile, _, err := r.FormFile("config")
	if err != nil {
		s.send(w, "fail", "config 파일이 필요합니다", http.StatusBadRequest)
		return
	}
	defer configFile.Close()

	configData, err := io.ReadAll(configFile)
	if err != nil {
		s.send(w, "fail", "config 읽기 실패", http.StatusInternalServerError)
		return
	}
	version, err := config.ParseVersionFromYAML(configData)
	if err != nil || version == "" {
		s.send(w, "fail", "config.yaml에서 version을 읽을 수 없습니다", http.StatusBadRequest)
		return
	}
	for _, c := range version {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' {
			continue
		}
		s.send(w, "fail", "version에 허용되지 않은 문자가 있습니다", http.StatusBadRequest)
		return
	}
	if version == "" || version == "." || version == ".." {
		s.send(w, "fail", "version이 비어 있거나 올바르지 않습니다", http.StatusBadRequest)
		return
	}

	base := s.deployBase
	if base == "" {
		base = "/opt/mol"
	}
	s.clearStaging(base)
	stagingDir, err := s.writeToStaging(base, version, molFile, configData)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("upload: version %s -> %s (staging)", version, stagingDir)
	s.send(w, "success", map[string]string{"version": version}, http.StatusOK)
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
	for _, c := range version {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' {
			continue
		}
		s.send(w, "fail", "version에 허용되지 않은 문자가 있습니다", http.StatusBadRequest)
		return
	}
	base := s.deployBase
	if base == "" {
		base = "/opt/mol"
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

// remoteHTTPClient is used to call another mol instance's upload/apply APIs (no SSH/SCP).
var remoteHTTPClient = &http.Client{Timeout: 120 * time.Second}

// postUploadToTarget sends mol and config from local paths to target mol's upload API (staging).
func (s *Server) postUploadToTarget(ctx context.Context, baseURL, apiPrefix, molPath, configPath string) error {
	molFile, err := os.Open(molPath)
	if err != nil {
		return fmt.Errorf("mol 파일 열기: %w", err)
	}
	defer molFile.Close()
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config 읽기: %w", err)
	}
	return s.postUploadToTargetWithReader(ctx, baseURL, apiPrefix, molFile, configData)
}

// postUploadToTargetWithReader sends mol (from reader) and configData to target mol's upload API (staging).
func (s *Server) postUploadToTargetWithReader(ctx context.Context, baseURL, apiPrefix string, molReader io.Reader, configData []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	molPart, err := w.CreateFormFile("mol", "mol")
	if err != nil {
		return err
	}
	if _, err := io.Copy(molPart, molReader); err != nil {
		return err
	}
	configPart, err := w.CreateFormFile("config", "config.yaml")
	if err != nil {
		return err
	}
	if _, err := configPart.Write(configData); err != nil {
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
		Status string `json:"status"`
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

// postApplyUpdateToTarget tells target mol to apply the given version from its staging (ip=self).
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
		base = "/opt/mol"
	}

	// 원격 전용: multipart(mol+config+ip) → 원격 mol 업로드 API로 전송 후 원격 apply-update API 호출 (로컬 스테이징·SCP 미사용)
	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			s.send(w, "fail", "multipart 파싱 실패", http.StatusBadRequest)
			return
		}
		ip := strings.TrimSpace(r.FormValue("ip"))
		if ip == "" || ip == "self" {
			s.send(w, "fail", "원격 적용 시 ip가 필요합니다", http.StatusBadRequest)
			return
		}
		molFile, _, err := r.FormFile("mol")
		if err != nil {
			s.send(w, "fail", "mol 파일이 필요합니다", http.StatusBadRequest)
			return
		}
		defer molFile.Close()
		configFile, _, err := r.FormFile("config")
		if err != nil {
			s.send(w, "fail", "config 파일이 필요합니다", http.StatusBadRequest)
			return
		}
		defer configFile.Close()
		configData, err := io.ReadAll(configFile)
		if err != nil {
			s.send(w, "fail", "config 읽기 실패", http.StatusInternalServerError)
			return
		}
		version, err := config.ParseVersionFromYAML(configData)
		if err != nil || version == "" {
			s.send(w, "fail", "config.yaml에서 version을 읽을 수 없습니다", http.StatusBadRequest)
			return
		}
		for _, c := range version {
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' {
				continue
			}
			s.send(w, "fail", "version에 허용되지 않은 문자가 있습니다", http.StatusBadRequest)
			return
		}
		port := s.servicePort
		if port <= 0 {
			port = 8888
		}
		baseURL := "http://" + ip + ":" + strconv.Itoa(port)
		ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
		defer cancel()
		if err := s.postUploadToTargetWithReader(ctx, baseURL, s.apiPrefix, molFile, configData); err != nil {
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
		log.Printf("apply-update: remote %s version %s applied (multipart -> upload API)", ip, version)
		s.send(w, "success", "원격 "+ip+" 에 버전 "+version+" 적용 완료. 서비스 상태를 새로고침하세요.", http.StatusOK)
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
	for _, c := range version {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' {
			continue
		}
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
		updateScript := filepath.Join(base, "update.sh")
		if _, err := os.Stat(updateScript); err != nil {
			s.send(w, "fail", "update.sh를 찾을 수 없습니다: "+updateScript, http.StatusOK)
			return
		}
		exec.Command("sudo", "systemctl", "reset-failed", "mol-update.service").Run()
		exec.Command("sudo", "systemctl", "stop", "mol-update.service").Run()
		logPath := filepath.Join(base, "update_last.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			s.send(w, "fail", "로그 파일 생성 실패: "+err.Error(), http.StatusOK)
			return
		}
		go func() {
			defer logFile.Close()
			cmd := exec.Command("sudo",
				"systemd-run",
				"--unit=mol-update",
				"--property=RemainAfterExit=yes",
				updateScript, version)
			cmd.Stdout = logFile
			cmd.Stderr = logFile
			if err := cmd.Run(); err != nil {
				log.Printf("apply-update: systemd-run %s %s: %v", updateScript, version, err)
			}
			// 스테이징은 자동 삭제하지 않음. 원격 업데이트에 재사용할 수 있도록 남겨 두고, 사용자가 「업로드된 버전 삭제」로 수동 삭제.
		}()
		log.Printf("apply-update: systemd-run --unit=mol-update %s %s (log: %s)", updateScript, version, logPath)
		s.send(w, "success", "업데이트를 적용 중입니다. 잠시 후 서버가 재시작됩니다. 아래 로그를 새로고침하세요.", http.StatusOK)
		return
	}

	s.doRemoteUpdate(w, ip, version, base, versionDir)
}

// doRemoteUpdate sends files to the remote mol's upload API (staging), then calls the remote's apply-update API (no SSH/SCP).
func (s *Server) doRemoteUpdate(w http.ResponseWriter, ip, version, base, versionDir string) {
	port := s.servicePort
	if port <= 0 {
		port = 8888
	}
	baseURL := "http://" + ip + ":" + strconv.Itoa(port)
	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
	defer cancel()

	molPath := filepath.Join(versionDir, "mol")
	configPath := filepath.Join(versionDir, "config.yaml")
	if err := s.postUploadToTarget(ctx, baseURL, s.apiPrefix, molPath, configPath); err != nil {
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
		base = "/opt/mol"
	}
	currentVersion := ""
	currentLink := filepath.Join(base, "current")
	if target, err := os.Readlink(currentLink); err == nil {
		// target is e.g. "versions/0.0.6" or absolute path ending with versions/0.0.6
		currentVersion = filepath.Base(target)
	}
	stagingParent := filepath.Join(base, "staging")
	stagingVersions := []string{}
	if entries, err := os.ReadDir(stagingParent); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			v := e.Name()
			molPath := filepath.Join(stagingParent, v, "mol")
			if _, err := os.Stat(molPath); err == nil {
				stagingVersions = append(stagingVersions, v)
			}
		}
	}
	var applyVersion, removeVersion string
	canApply := false
	for _, v := range stagingVersions {
		if v != currentVersion {
			canApply = true
			if applyVersion == "" {
				applyVersion = v
			}
		}
		if removeVersion == "" {
			removeVersion = v
		}
	}
	s.send(w, "success", map[string]interface{}{
		"current_version":  currentVersion,
		"staging_versions": stagingVersions,
		"can_apply":        canApply,
		"apply_version":    applyVersion,
		"remove_version":   removeVersion,
	}, http.StatusOK)
}

func (s *Server) handleUpdateLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	base := s.deployBase
	if base == "" {
		base = "/opt/mol"
	}
	logPath := filepath.Join(base, "update_last.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.send(w, "success", map[string]string{"output": "(아직 실행 기록 없음)"}, http.StatusOK)
			return
		}
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", map[string]string{"output": string(data)}, http.StatusOK)
}

func (s *Server) send(w http.ResponseWriter, status string, data interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(APIResponse{Status: status, Data: data})
}
