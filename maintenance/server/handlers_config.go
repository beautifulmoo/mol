package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"contrabass-agent/maintenance/appmeta"
)

// currentConfigPath returns the path to deploy_base/current/<config file> (current symlink resolved), or "" if not available.
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
	return filepath.Join(resolved, appmeta.ConfigFileName)
}

func (s *Server) handleCurrentConfig(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	var postContent string
	var backupBeforeWrite bool
	if r.Method == http.MethodPost {
		var reqBody struct {
			IP                string `json:"ip"`
			Content           string `json:"content"`
			BackupBeforeWrite bool   `json:"backup_before_write"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			s.send(w, "fail", "invalid body", http.StatusBadRequest)
			return
		}
		postContent = reqBody.Content
		backupBeforeWrite = reqBody.BackupBeforeWrite
		if strings.TrimSpace(reqBody.IP) != "" {
			ip = strings.TrimSpace(reqBody.IP)
		}
	}
	if ip != "" && ip != "self" {
		if r.Method == http.MethodGet {
			s.proxyRemoteAPI(w, ip, "remote config request failed: ", "", http.MethodGet, "/current-config", nil)
			return
		}
		if r.Method == http.MethodPost {
			payload, _ := json.Marshal(map[string]interface{}{
				"content":             postContent,
				"backup_before_write": true,
			})
			s.proxyRemoteAPI(w, ip, "remote config request failed: ", "remote config save request failed: ", http.MethodPost, "/current-config", payload)
			return
		}
	}
	configPath := s.currentConfigPath()
	if configPath == "" {
		s.send(w, "fail", "current version symlink not found", http.StatusOK)
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
			s.send(w, "fail", appmeta.ConfigFileName+" read failed: "+err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", map[string]interface{}{"content": string(data)}, http.StatusOK)
		return
	case http.MethodPost:
		if err := saveCurrentConfigContent(configPath, postContent, backupBeforeWrite); err != nil {
			s.send(w, "fail", err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", nil, http.StatusOK)
		return
	default:
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCurrentConfigPushLocal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(req.IP)
	if ip == "" || ip == "self" {
		s.send(w, "fail", "remote ip is required", http.StatusBadRequest)
		return
	}
	content, err := s.readLocalCurrentConfigContent()
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	if err := s.pushConfigContentToRemote(content, ip); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", map[string]string{
		"message": "local current " + appmeta.ConfigFileName + " pushed to " + ip,
	}, http.StatusOK)
}
