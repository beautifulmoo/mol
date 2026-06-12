package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/server/remoteregistry"
)

type bulkApplyUpdateRequest struct {
	Hosts               []pushHostInput `json:"hosts"`
	IPs                 []string        `json:"ips"`
	Version             string          `json:"version"`
	AgentVariant        string          `json:"agent_variant"`
	ReusePreviousConfig *bool           `json:"reuse_previous_config"`
}

func parseBulkApplyUpdateRequest(r *http.Request) (bulkApplyUpdateRequest, error) {
	var req bulkApplyUpdateRequest
	if r.Body == nil {
		return req, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		return req, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	return req, nil
}

func (s *Server) handleApplyUpdateAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	req, err := parseBulkApplyUpdateRequest(r)
	if err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}

	version := strings.TrimSpace(req.Version)
	if version == "" {
		staging := s.localStagingVersions()
		if len(staging) == 0 {
			s.send(w, "fail", "no staged version to apply", http.StatusOK)
			return
		}
		version = staging[0]
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

	reusePreviousConfig := true
	if req.ReusePreviousConfig != nil {
		reusePreviousConfig = *req.ReusePreviousConfig
	}

	base := s.deployBaseOrDefault()
	versionDir, _ := s.resolveVersionDir(base, version)
	if versionDir == "" {
		s.send(w, "fail", "version not found in staging or versions/: "+version, http.StatusOK)
		return
	}

	hosts := s.remotesForConfigPush(req.Hosts, req.IPs)

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	total := len(hosts)
	if err := writeNDJSONLine(w, flusher, map[string]interface{}{
		"type":    "start",
		"total":   total,
		"version": version,
	}); err != nil {
		return
	}

	succeeded := 0
	failed := 0
	skipped := 0
	for i, remote := range hosts {
		displayIP := bulkDisplayIP(remote)
		evt := map[string]interface{}{
			"type":     "progress",
			"current":  i + 1,
			"total":    total,
			"ip":       displayIP,
			"hostname": remote.Hostname,
			"cpu_uuid": remote.CPUUUID,
			"version":  version,
		}

		remoteVersion, verErr := s.fetchRemoteVersionKeyForHost(remote)
		if verErr != nil {
			failed++
			evt["status"] = "fail"
			evt["message"] = "remote version query failed: " + verErr.Error()
			_ = writeNDJSONLine(w, flusher, evt)
			continue
		}
		if !agentcfg.StagingUpdateAvailable(version, remoteVersion, s.allowSameVersionUpdate) {
			skipped++
			evt["status"] = "skipped"
			evt["message"] = fmt.Sprintf("원격 버전(%s)에 스테이징 %s 적용 불가", remoteVersion, version)
			evt["remote_version"] = remoteVersion
			_ = writeNDJSONLine(w, flusher, evt)
			continue
		}

		connectIP, tried, applyErr := s.applyUpdateOnRemoteHost(remote, version, versionDir, agentVariant, reusePreviousConfig)
		if len(tried) > 0 {
			evt["tried_ips"] = tried
		}
		if applyErr != nil {
			failed++
			evt["status"] = "fail"
			evt["message"] = applyErr.Error()
		} else {
			succeeded++
			evt["status"] = "success"
			evt["connect_ip"] = connectIP
			evt["remote_version"] = remoteVersion
		}
		if err := writeNDJSONLine(w, flusher, evt); err != nil {
			return
		}
	}
	if err := s.appendDeployHistory(fmt.Sprintf("apply-update-all finished succeeded=%d failed=%d skipped=%d", succeeded, failed, skipped)); err != nil {
		log.Printf("update_history: apply-update-all finish: %v", err)
	}
	_ = writeNDJSONLine(w, flusher, map[string]interface{}{
		"type":      "done",
		"total":     total,
		"succeeded": succeeded,
		"failed":    failed,
		"skipped":   skipped,
		"version":   version,
	})
}

func bulkDisplayIP(remote remoteregistry.Remote) string {
	displayIP := strings.TrimSpace(remote.PrimaryIP)
	if displayIP == "" {
		ips := remoteregistry.ConnectIPs(remote)
		if len(ips) > 0 {
			displayIP = ips[0]
		}
	}
	return displayIP
}

func (s *Server) fetchRemoteVersionKeyForHost(remote remoteregistry.Remote) (string, error) {
	ips := remoteregistry.ConnectIPs(remote)
	var lastErr error
	for _, ip := range ips {
		rv, err := s.fetchRemoteVersionKey(ip)
		if err == nil {
			s.remoteRegistry.UpsertFromRemoteIP(ip)
			return strings.TrimSpace(rv), nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no connect ip for host")
}
