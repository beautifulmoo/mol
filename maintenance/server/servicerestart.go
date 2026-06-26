package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	payload, _ := json.Marshal(map[string]string{"ip": "self", "action": "restart"})
	out, err := s.fetchRemoteAPI(ip, http.MethodPost, "/service-control", payload)
	if err != nil {
		return "", err
	}
	if out.Status == "success" {
		return "", nil
	}
	msg := apiResponseErrorMessage(out)
	return msg, fmt.Errorf("%s", msg)
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
	out, err := s.fetchRemoteAPI(ip, http.MethodGet, "/service-status", nil)
	if err != nil {
		return "", err
	}
	if out.Status != "success" {
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
	return tryRemoteHostWithDetail(remote, s.restartRemoteAtIP)
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
	hosts := s.bulkRemoteHosts(req.Hosts, req.IPs)

	s.runBulkRemoteNDJSON(w, hosts, bulkNDJSONOptions{
		HistoryFmt: func(sum bulkRunSummary) string {
			return fmt.Sprintf("service restart-all finished succeeded=%d failed=%d", sum.Succeeded, sum.Failed)
		},
	}, func(remote remoteregistry.Remote, evt map[string]interface{}) bulkHostOutcome {
		connectIP, tried, verifyDetail, restartErr := s.restartRemoteHost(remote)
		if restartErr != nil {
			extra := map[string]interface{}{"verify_ok": false}
			if verifyDetail != "" {
				extra["verify_detail"] = verifyDetail
			}
			return bulkHostOutcome{Status: bulkHostFail, Message: restartErr.Error(), TriedIPs: tried, Extra: extra}
		}
		return bulkHostOutcome{
			Status:    bulkHostSuccess,
			ConnectIP: connectIP,
			TriedIPs:  tried,
			Extra: map[string]interface{}{
				"verify_ok":     true,
				"verify_detail": verifyDetail,
			},
		}
	})
}
