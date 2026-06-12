package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"contrabass-agent/maintenance/server/remoteregistry"
)

const (
	remoteRestartInitialWait    = 2 * time.Second
	remoteRestartVerifyInterval = 2 * time.Second
	remoteRestartVerifyTimeout  = 45 * time.Second
)

var serviceActiveRunningRe = regexp.MustCompile(`(?i)Active:\s*active\s*\(running\)`)

func isRestartInProgressTransport(err error, apiMsg string) bool {
	s := strings.ToLower(strings.TrimSpace(apiMsg))
	if err != nil {
		s += " " + strings.ToLower(err.Error())
	}
	return strings.Contains(s, "connection reset") ||
		strings.Contains(s, "eof") ||
		strings.Contains(s, "terminated") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "remote restart request failed")
}

func (s *Server) postRemoteRestart(ip string) (string, error) {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return "", err
	}
	u := baseURL + s.apiPrefix + "/service-control"
	payload, _ := json.Marshal(map[string]string{"ip": "self", "action": "restart"})
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out APIResponse
	if json.Unmarshal(body, &out) != nil {
		return "", fmt.Errorf("failed to parse remote response")
	}
	if out.Status == "success" {
		return "", nil
	}
	return apiResponseErrorMessage(out), fmt.Errorf("%s", apiResponseErrorMessage(out))
}

func (s *Server) remoteHealthOK(ip string) bool {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return false
	}
	u := baseURL + s.apiPrefix + "/health"
	timeout := time.Duration(s.remoteHealthTimeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	var out APIResponse
	return json.Unmarshal(body, &out) == nil && out.Status == "success"
}

// fetchRemoteServiceStatusText proxies GET service-status to the remote agent.
func (s *Server) fetchRemoteServiceStatusText(ip string) (string, error) {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return "", err
	}
	u := baseURL + s.apiPrefix + "/service-status"
	resp, err := remoteHTTPClient.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out APIResponse
	if json.Unmarshal(body, &out) != nil || out.Status != "success" {
		return "", fmt.Errorf("remote service-status failed")
	}
	switch v := out.Data.(type) {
	case string:
		return v, nil
	case map[string]interface{}:
		if o, ok := v["output"].(string); ok {
			return o, nil
		}
	}
	return "", fmt.Errorf("unexpected service-status payload")
}

func serviceStatusOutputActive(output string) bool {
	return serviceActiveRunningRe.MatchString(output)
}

func (s *Server) waitRemoteServiceHealthy(ip string) (bool, string) {
	time.Sleep(remoteRestartInitialWait)
	deadline := time.Now().Add(remoteRestartVerifyTimeout)
	lastNote := "timeout"
	for time.Now().Before(deadline) {
		if s.remoteHealthOK(ip) {
			return true, "HTTP health check ok"
		}
		if out, err := s.fetchRemoteServiceStatusText(ip); err == nil {
			if serviceStatusOutputActive(out) {
				return true, "systemctl active (running)"
			}
			lastNote = "service not active yet"
		} else {
			lastNote = err.Error()
		}
		time.Sleep(remoteRestartVerifyInterval)
	}
	return false, lastNote
}

func (s *Server) restartRemoteAtIP(ip string) (verifyDetail string, err error) {
	apiMsg, reqErr := s.postRemoteRestart(ip)
	if reqErr != nil && !isRestartInProgressTransport(reqErr, apiMsg) {
		if apiMsg != "" {
			return "", fmt.Errorf("%s", apiMsg)
		}
		return "", reqErr
	}
	ok, detail := s.waitRemoteServiceHealthy(ip)
	if !ok {
		return detail, fmt.Errorf("verification failed: %s", detail)
	}
	return detail, nil
}

func (s *Server) restartRemoteHost(remote remoteregistry.Remote) (connectIP string, tried []string, verifyDetail string, err error) {
	ips := remoteregistry.ConnectIPs(remote)
	if len(ips) == 0 {
		return "", nil, "", fmt.Errorf("no connect ip for host")
	}
	var lastErr error
	var lastDetail string
	for _, ip := range ips {
		tried = append(tried, ip)
		detail, restartErr := s.restartRemoteAtIP(ip)
		if restartErr == nil {
			return ip, tried, detail, nil
		}
		lastErr = restartErr
		lastDetail = detail
	}
	if lastDetail != "" && lastErr != nil {
		return "", tried, lastDetail, lastErr
	}
	return "", tried, lastDetail, lastErr
}

func (s *Server) handleServiceRestartAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	req, err := parseBulkHostsRequest(r)
	if err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	hosts := s.remotesForConfigPush(req.Hosts, req.IPs)

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	total := len(hosts)
	if err := writeNDJSONLine(w, flusher, map[string]interface{}{
		"type":  "start",
		"total": total,
	}); err != nil {
		return
	}

	succeeded := 0
	failed := 0
	for i, remote := range hosts {
		displayIP := strings.TrimSpace(remote.PrimaryIP)
		if displayIP == "" {
			ips := remoteregistry.ConnectIPs(remote)
			if len(ips) > 0 {
				displayIP = ips[0]
			}
		}
		evt := map[string]interface{}{
			"type":     "progress",
			"current":  i + 1,
			"total":    total,
			"ip":       displayIP,
			"hostname": remote.Hostname,
			"cpu_uuid": remote.CPUUUID,
		}
		connectIP, tried, verifyDetail, restartErr := s.restartRemoteHost(remote)
		if len(tried) > 0 {
			evt["tried_ips"] = tried
		}
		if restartErr != nil {
			failed++
			evt["status"] = "fail"
			evt["verify_ok"] = false
			evt["message"] = restartErr.Error()
			if verifyDetail != "" {
				evt["verify_detail"] = verifyDetail
			}
		} else {
			succeeded++
			evt["status"] = "success"
			evt["verify_ok"] = true
			evt["connect_ip"] = connectIP
			evt["verify_detail"] = verifyDetail
		}
		if err := writeNDJSONLine(w, flusher, evt); err != nil {
			return
		}
	}
	if err := s.appendDeployHistory(fmt.Sprintf("service restart-all finished succeeded=%d failed=%d", succeeded, failed)); err != nil {
		log.Printf("update_history: service restart-all finish: %v", err)
	}
	_ = writeNDJSONLine(w, flusher, map[string]interface{}{
		"type":      "done",
		"total":     total,
		"succeeded": succeeded,
		"failed":    failed,
	})
}
