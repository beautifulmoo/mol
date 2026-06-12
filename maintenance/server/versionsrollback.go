package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"contrabass-agent/maintenance/versionsapi"
)

func (s *Server) handleVersionsRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IP string `json:"ip"`
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			s.send(w, "fail", "invalid body", http.StatusBadRequest)
			return
		}
		if len(bytes.TrimSpace(body)) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				s.send(w, "fail", "invalid body", http.StatusBadRequest)
				return
			}
		}
	}

	ip := strings.TrimSpace(req.IP)
	if ip != "" && ip != "self" {
		baseURL, err := s.remoteBaseURL(ip)
		if err != nil {
			s.send(w, "fail", "remote request failed: "+err.Error(), http.StatusOK)
			return
		}
		u := strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(s.apiPrefix, "/") + "/versions/rollback"
		hr, err := http.NewRequest(http.MethodPost, u, bytes.NewReader([]byte("{}")))
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusInternalServerError)
			return
		}
		hr.Header.Set("Content-Type", "application/json")
		resp, err := remoteHTTPClient.Do(hr)
		if err != nil {
			s.send(w, "fail", "remote rollback request failed: "+err.Error(), http.StatusOK)
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

	base := s.deployBaseOrDefault()
	if err := versionsapi.RunEmbeddedRollback(base); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", "Rollback in progress; the service will restart shortly. Refresh the update log below.", http.StatusOK)
}
