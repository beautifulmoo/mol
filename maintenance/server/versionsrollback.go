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
		s.proxyRemoteAPI(w, ip, "remote request failed: ", "remote rollback request failed: ", http.MethodPost, "/versions/rollback", []byte("{}"))
		return
	}

	base := s.deployBaseOrDefault()
	if err := versionsapi.RunEmbeddedRollback(base); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", "Rollback in progress; the service will restart shortly. Refresh the update log below.", http.StatusOK)
}
