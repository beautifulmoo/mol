package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/versionsapi"
)

func (s *Server) handleVersionsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip != "" && ip != "self" {
		out, err := s.fetchRemoteAPI(ip, http.MethodGet, "/versions/list", nil)
		if err != nil {
			sendRemoteAPIError(w, s.send, "remote versions list request failed: ", err)
			return
		}
		if out.Status == "success" {
			s.send(w, "success", versionsapi.EnrichVersionsListData(out.Data), http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}
	base := s.versionsBase()
	list, err := versionsapi.ListInstalledVersions(base)
	if err != nil {
		s.send(w, "fail", "cannot read versions directory: "+err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", versionsapi.VersionsListData(list), http.StatusOK)
}

// resolveSymlinkVersion returns the version name (dir under base/versions/) that the symlink base/name points to, or "".
func (s *Server) resolveSymlinkVersion(base, name string) string {
	return versionsapi.ResolveSymlinkVersion(base, name)
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
			if err := agentcfg.ValidateVersionKeyPath(ver); err != nil {
				s.send(w, "fail", ver+": "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		payload, _ := json.Marshal(map[string]interface{}{"versions": req.Versions})
		s.proxyRemoteAPI(w, ip, "remote versions remove request failed: ", "", http.MethodPost, "/versions/remove", payload)
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
		if err := agentcfg.ValidateVersionKeyPath(ver); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s (%v)", ver, err))
			continue
		}
		if ver == currentVer {
			skipped = append(skipped, ver+" (current)")
			continue
		}
		if ver == previousVer {
			skipped = append(skipped, ver+" (previous, for rollback)")
			continue
		}
		dir := s.versionsDir(base, ver)
		clean := filepath.Clean(dir)
		rel, relErr := filepath.Rel(versionsParent, clean)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, "..") || clean == versionsParent {
			skipped = append(skipped, ver+" (invalid path)")
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
		msg = "removed: " + strings.Join(removed, ", ")
	}
	if len(skipped) > 0 {
		if msg != "" {
			msg += ". "
		}
		msg += "skipped: " + strings.Join(skipped, "; ")
	}
	if msg == "" {
		msg = "no versions selected for removal."
	}
	s.send(w, "success", msg, http.StatusOK)
}

// handleVersionsSwitchCurrent POST body: { "version": "<키>", "ip": "" | "self" | "<원격>" } — 지정 버전을 current로 두기 위해
// 내장 update.sh를 systemd-run으로 실행한다(apply-update 로컬 경로와 동일). 원격 ip면 해당 호스트 API로 프록시.
func (s *Server) handleVersionsSwitchCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
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
		s.send(w, "fail", "version is required", http.StatusBadRequest)
		return
	}
	if err := agentcfg.ValidateVersionKeyPath(version); err != nil {
		s.send(w, "fail", "version contains invalid characters", http.StatusBadRequest)
		return
	}
	ip := strings.TrimSpace(req.IP)
	if ip != "" && ip != "self" {
		payload, err := json.Marshal(map[string]string{"version": version})
		if err != nil {
			s.send(w, "fail", err.Error(), http.StatusInternalServerError)
			return
		}
		s.proxyRemoteAPI(w, ip, "remote request failed: ", "remote switch-current request failed: ", http.MethodPost, "/versions/switch-current", payload)
		return
	}

	base := s.deployBaseOrDefault()
	if dir, _ := s.resolveVersionDir(base, version); dir == "" {
		s.send(w, "fail", "version not found in staging or versions/: "+version, http.StatusOK)
		return
	}
	if err := s.runUpdateViaEmbeddedScript(base, version, "", false); err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", "systemd-run started update.sh. Service restart and health checks may take tens of seconds. On failure check update_history.log and journal.", http.StatusOK)
}
