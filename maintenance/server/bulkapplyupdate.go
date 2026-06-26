package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	s.runBulkRemoteNDJSON(w, hosts, bulkNDJSONOptions{
		StartExtra: map[string]interface{}{"version": version},
		DoneExtra: func(sum bulkRunSummary) map[string]interface{} {
			return map[string]interface{}{
				"skipped": sum.Skipped,
				"version": version,
			}
		},
		HistoryFmt: func(sum bulkRunSummary) string {
			return fmt.Sprintf("apply-update-all finished succeeded=%d failed=%d skipped=%d", sum.Succeeded, sum.Failed, sum.Skipped)
		},
	}, func(remote remoteregistry.Remote, evt map[string]interface{}) bulkHostOutcome {
		evt["version"] = version

		remoteVersion, verErr := s.fetchRemoteVersionKeyForHost(remote)
		if verErr != nil {
			return bulkHostOutcome{
				Status:               bulkHostFail,
				Message:              "remote version query failed: " + verErr.Error(),
				ContinueOnWriteError: true,
			}
		}
		if !agentcfg.StagingUpdateAvailable(version, remoteVersion, s.allowSameVersionUpdate) {
			return bulkHostOutcome{
				Status:               bulkHostSkipped,
				Message:              fmt.Sprintf("원격 버전(%s)에 스테이징 %s 적용 불가", remoteVersion, version),
				ContinueOnWriteError: true,
				Extra:                map[string]interface{}{"remote_version": remoteVersion},
			}
		}

		connectIP, tried, applyErr := s.applyUpdateOnRemoteHost(remote, version, versionDir, agentVariant, reusePreviousConfig)
		if applyErr != nil {
			return bulkHostOutcome{
				Status:  bulkHostFail,
				Message: applyErr.Error(),
				TriedIPs: tried,
				Extra:   map[string]interface{}{"remote_version": remoteVersion},
			}
		}
		return bulkHostOutcome{
			Status:    bulkHostSuccess,
			ConnectIP: connectIP,
			TriedIPs:  tried,
			Extra:     map[string]interface{}{"remote_version": remoteVersion},
		}
	})
}

func (s *Server) fetchRemoteVersionKeyForHost(remote remoteregistry.Remote) (string, error) {
	return tryRemoteHostFirstReachable(remote, func(ip string) (string, error) {
		rv, err := s.fetchRemoteVersionKey(ip)
		if err == nil {
			s.remoteRegistry.UpsertFromRemoteIP(ip)
		}
		return strings.TrimSpace(rv), err
	})
}
