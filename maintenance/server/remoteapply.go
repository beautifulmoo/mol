package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"contrabass-agent/maintenance/server/remoteregistry"
	"contrabass-agent/maintenance/versionsapi"
)

const (
	remoteApplyTimeoutDefault    = 115 * time.Second
	remoteApplyTimeoutMultipart  = 280 * time.Second
	remoteStagingSubdir          = "remote-staging"
)

func (s *Server) applyUpdateOnRemote(ip, version, versionDir, agentVariant string, reusePreviousConfig bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteApplyTimeoutDefault)
	defer cancel()
	return s.applyUpdateOnRemoteCtx(ctx, ip, version, versionDir, agentVariant, reusePreviousConfig)
}

func (s *Server) applyUpdateOnRemoteCtx(ctx context.Context, ip, version, versionDir, agentVariant string, reusePreviousConfig bool) error {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return fmt.Errorf("remote apply failed: %w", err)
	}

	if !versionsapi.DirHasAgentBinary(versionDir) && !versionsapi.StagingHasDualAgents(versionDir) {
		return fmt.Errorf("version directory missing agent binaries: %s", versionDir)
	}
	if reusePreviousConfig {
		if err := s.injectRemoteReuseConfigIntoVersionDir(ctx, baseURL, s.apiPrefix, versionDir); err != nil {
			return fmt.Errorf("reuse remote config: %w", err)
		}
	}
	if err := s.postUploadToTarget(ctx, baseURL, s.apiPrefix, versionDir); err != nil {
		return err
	}
	status, data, err := s.postApplyUpdateToTarget(ctx, baseURL, s.apiPrefix, version, agentVariant, reusePreviousConfig)
	if err != nil {
		return err
	}
	if status != "success" {
		msg := "remote apply failed"
		if msgStr, ok := data.(string); ok && msgStr != "" {
			msg = msgStr
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// applyPreparedBundleOnRemote stages a validated upload bundle then runs the shared remote apply path.
// Used by multipart POST /apply-update; timeout preserves the longer multipart budget (upload + apply).
func (s *Server) applyPreparedBundleOnRemote(ip string, pb *PreparedBundle, agentVariant string, reusePreviousConfig bool, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = remoteApplyTimeoutDefault
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	versionDir, err := s.stagePreparedBundleForRemoteApply(pb, reusePreviousConfig)
	if err != nil {
		return err
	}
	return s.applyUpdateOnRemoteCtx(ctx, ip, pb.VersionKey, versionDir, agentVariant, reusePreviousConfig)
}

// stagePreparedBundleForRemoteApply materializes pb under WorkDir/remote-staging for remote upload.
// When reusePreviousConfig is false and BundlePath is set, the original tar.gz is kept as StagedBundleFileName
// so postUploadToTarget re-sends the same bytes (multipart non-reuse behavior).
// When reusePreviousConfig is true, config replacement is deferred to injectRemoteReuseConfigIntoVersionDir.
func (s *Server) stagePreparedBundleForRemoteApply(pb *PreparedBundle, reusePreviousConfig bool) (string, error) {
	if pb == nil {
		return "", fmt.Errorf("prepared bundle is nil")
	}
	uploadDir := filepath.Join(pb.WorkDir, remoteStagingSubdir)
	if err := os.RemoveAll(uploadDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", fmt.Errorf("remote staging dir: %w", err)
	}
	if err := StagePreparedBundle(uploadDir, pb); err != nil {
		return "", err
	}
	if !reusePreviousConfig && strings.TrimSpace(pb.BundlePath) != "" {
		dst := filepath.Join(uploadDir, StagedBundleFileName)
		if err := copyFile(pb.BundlePath, dst, 0644); err != nil {
			return "", fmt.Errorf("save uploaded bundle: %w", err)
		}
	}
	return uploadDir, nil
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

func (s *Server) applyUpdateOnRemoteHost(remote remoteregistry.Remote, version, versionDir, agentVariant string, reusePreviousConfig bool) (connectIP string, tried []string, err error) {
	return tryRemoteHostVoid(remote, func(ip string) error {
		return s.applyUpdateOnRemote(ip, version, versionDir, agentVariant, reusePreviousConfig)
	})
}

func (s *Server) postRollbackToTarget(ctx context.Context, baseURL, apiPrefix string) (status string, data interface{}, err error) {
	if !strings.HasPrefix(apiPrefix, "/") {
		apiPrefix = "/" + apiPrefix
	}
	rollbackURL := baseURL + apiPrefix + "/versions/rollback"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rollbackURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("remote rollback request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	var out APIResponse
	if json.Unmarshal(respBody, &out) != nil {
		return "", nil, errRemoteAPIParse
	}
	return out.Status, out.Data, nil
}

func (s *Server) rollbackOnRemoteIP(ip string) error {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return fmt.Errorf("remote rollback failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteApplyTimeoutDefault)
	defer cancel()
	status, data, err := s.postRollbackToTarget(ctx, baseURL, s.apiPrefix)
	if err != nil {
		return err
	}
	if status != "success" {
		msg := "remote rollback failed"
		if msgStr, ok := data.(string); ok && msgStr != "" {
			msg = msgStr
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (s *Server) rollbackOnRemoteHost(remote remoteregistry.Remote) (connectIP string, tried []string, err error) {
	return tryRemoteHostVoid(remote, s.rollbackOnRemoteIP)
}

func (s *Server) remoteSymlinkVersionKeysAtIP(ip string) (currentKey, previousKey string, err error) {
	url, err := s.remoteAPIURL(ip, "/versions/list")
	if err != nil {
		return "", "", err
	}
	out, err := callRemoteAPIAtURL(http.MethodGet, url, nil)
	if err != nil || out.Status != "success" {
		return "", "", fmt.Errorf("remote versions list failed")
	}
	dataMap, ok := out.Data.(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("unexpected versions list payload")
	}
	versions, ok := dataMap["versions"].([]interface{})
	if !ok {
		return "", "", nil
	}
	for _, item := range versions {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		ver, _ := row["version"].(string)
		if cur, ok := row["is_current"].(bool); ok && cur {
			currentKey = strings.TrimSpace(ver)
		}
		if prev, ok := row["is_previous"].(bool); ok && prev {
			previousKey = strings.TrimSpace(ver)
		}
	}
	return currentKey, previousKey, nil
}

// remoteCanRollbackAtIP is true when previous exists and resolves to a different version than current.
func (s *Server) remoteCanRollbackAtIP(ip string) (bool, error) {
	currentKey, previousKey, err := s.remoteSymlinkVersionKeysAtIP(ip)
	if err != nil {
		return false, err
	}
	if previousKey == "" {
		return false, nil
	}
	return currentKey != previousKey, nil
}

func (s *Server) remoteHostCanRollback(remote remoteregistry.Remote) (bool, error) {
	return tryRemoteHostFirstReachable(remote, s.remoteCanRollbackAtIP)
}
