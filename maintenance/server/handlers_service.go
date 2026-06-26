package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"contrabass-agent/maintenance/svcstatus"
)

func (s *Server) handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	svcName := s.systemctlServiceName
	if svcName == "" {
		svcName = "contrabass-mole.service"
	}
	if ip != "" && ip != "self" {
		s.proxyRemoteAPI(w, ip, "remote service-status request failed: ", "", http.MethodGet, "/service-status", nil)
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
	Action string `json:"action"` // "start", "stop", or "restart"
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
	if action != "start" && action != "stop" && action != "restart" {
		s.send(w, "fail", "action must be start, stop, or restart", http.StatusBadRequest)
		return
	}
	svcName := s.systemctlServiceName
	if svcName == "" {
		svcName = "contrabass-mole.service"
	}
	if ip != "" && ip != "self" {
		if action == "restart" {
			payload, _ := json.Marshal(map[string]string{"ip": "self", "action": "restart"})
			s.proxyRemoteAPI(w, ip, "remote restart request failed: ", "", http.MethodPost, "/service-control", payload)
			return
		}
		// 시작/중지는 SSH로 실행 (서비스 중지 시 API 호출 불가)
		sshPort := s.sshPort
		if sshPort <= 0 {
			sshPort = 22
		}
		sshUser := s.sshUser
		if sshUser == "" {
			sshUser = "root"
		}
		err := svcstatus.RunRemote(ip, sshUser, sshPort, svcName, action)
		if err != nil {
			s.send(w, "fail", "remote SSH control failed: "+err.Error(), http.StatusOK)
			return
		}
		s.send(w, "success", nil, http.StatusOK)
		return
	}
	var err error
	switch action {
	case "start":
		err = svcstatus.StartLocal(svcName)
	case "stop":
		err = svcstatus.StopLocal(svcName)
	default:
		err = svcstatus.RestartLocal(svcName)
	}
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.send(w, "success", nil, http.StatusOK)
}
