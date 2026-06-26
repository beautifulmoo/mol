package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/versionsapi"
)

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	base := s.deployBaseOrDefault()

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
	base := s.deployBaseOrDefault()
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

	uploadURL := joinRemoteAPIURL(baseURL, apiPrefix, "/upload")
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
	applyURL := joinRemoteAPIURL(baseURL, apiPrefix, "/apply-update")
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

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	base := s.deployBaseOrDefault()

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

		if err := s.applyPreparedBundleOnRemote(ip, pb, agentVariant, reusePreviousConfig, remoteApplyTimeoutMultipart); err != nil {
			s.send(w, "fail", err.Error(), http.StatusOK)
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
