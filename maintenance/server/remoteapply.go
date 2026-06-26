package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"contrabass-agent/maintenance/server/remoteregistry"
	"contrabass-agent/maintenance/versionsapi"
)

func (s *Server) applyUpdateOnRemote(ip, version, versionDir, agentVariant string, reusePreviousConfig bool) error {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return fmt.Errorf("remote apply failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
	defer cancel()

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

func (s *Server) applyUpdateOnRemoteHost(remote remoteregistry.Remote, version, versionDir, agentVariant string, reusePreviousConfig bool) (connectIP string, tried []string, err error) {
	return tryRemoteHostVoid(remote, func(ip string) error {
		return s.applyUpdateOnRemote(ip, version, versionDir, agentVariant, reusePreviousConfig)
	})
}

func (s *Server) postRollbackToTarget(ctx context.Context, baseURL, apiPrefix string) (status string, data interface{}, err error) {
	rollbackURL := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(apiPrefix, "/") + "/versions/rollback"
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
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Status string      `json:"status"`
		Data   interface{} `json:"data"`
	}
	if json.Unmarshal(body, &out) != nil {
		return "", nil, fmt.Errorf("failed to parse remote response")
	}
	return out.Status, out.Data, nil
}

func (s *Server) rollbackOnRemoteIP(ip string) error {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return fmt.Errorf("remote rollback failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
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
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return "", "", err
	}
	u := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(s.apiPrefix, "/") + "/versions/list"
	resp, err := remoteHTTPClient.Get(u)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out APIResponse
	if json.Unmarshal(body, &out) != nil || out.Status != "success" {
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
