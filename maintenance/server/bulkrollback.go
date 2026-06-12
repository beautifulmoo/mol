package server

import (
	"fmt"
	"log"
	"net/http"
)

func (s *Server) handleRollbackAll(w http.ResponseWriter, r *http.Request) {
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
		}

		hasPrevious, prevErr := s.remoteHostHasPrevious(remote)
		if prevErr != nil {
			failed++
			evt["status"] = "fail"
			evt["message"] = "previous version check failed: " + prevErr.Error()
			_ = writeNDJSONLine(w, flusher, evt)
			continue
		}
		if !hasPrevious {
			skipped++
			evt["status"] = "skipped"
			evt["message"] = "이전(previous) 버전 없음"
			_ = writeNDJSONLine(w, flusher, evt)
			continue
		}

		connectIP, tried, rbErr := s.rollbackOnRemoteHost(remote)
		if len(tried) > 0 {
			evt["tried_ips"] = tried
		}
		if rbErr != nil {
			failed++
			evt["status"] = "fail"
			evt["message"] = rbErr.Error()
		} else {
			succeeded++
			evt["status"] = "success"
			evt["connect_ip"] = connectIP
		}
		if err := writeNDJSONLine(w, flusher, evt); err != nil {
			return
		}
	}
	if err := s.appendDeployHistory(fmt.Sprintf("rollback-all finished succeeded=%d failed=%d skipped=%d", succeeded, failed, skipped)); err != nil {
		log.Printf("update_history: rollback-all finish: %v", err)
	}
	_ = writeNDJSONLine(w, flusher, map[string]interface{}{
		"type":      "done",
		"total":     total,
		"succeeded": succeeded,
		"failed":    failed,
		"skipped":   skipped,
	})
}
