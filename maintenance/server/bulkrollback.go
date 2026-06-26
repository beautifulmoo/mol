package server

import (
	"fmt"
	"net/http"
	"contrabass-agent/maintenance/server/remoteregistry"
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
	hosts := s.bulkRemoteHosts(req.Hosts, req.IPs)

	s.runBulkRemoteNDJSON(w, hosts, bulkNDJSONOptions{
		DoneExtra: func(sum bulkRunSummary) map[string]interface{} {
			return map[string]interface{}{"skipped": sum.Skipped}
		},
		HistoryFmt: func(sum bulkRunSummary) string {
			return fmt.Sprintf("rollback-all finished succeeded=%d failed=%d skipped=%d", sum.Succeeded, sum.Failed, sum.Skipped)
		},
	}, func(remote remoteregistry.Remote, evt map[string]interface{}) bulkHostOutcome {
		canRollback, rbCheckErr := s.remoteHostCanRollback(remote)
		if rbCheckErr != nil {
			return bulkHostOutcome{
				Status:               bulkHostFail,
				Message:              "rollback eligibility check failed: " + rbCheckErr.Error(),
				ContinueOnWriteError: true,
			}
		}
		if !canRollback {
			return bulkHostOutcome{
				Status:               bulkHostSkipped,
				Message:              "롤백 불가 (current·previous 동일 또는 previous 없음)",
				ContinueOnWriteError: true,
			}
		}

		connectIP, tried, rbErr := s.rollbackOnRemoteHost(remote)
		if rbErr != nil {
			return bulkHostOutcome{Status: bulkHostFail, Message: rbErr.Error(), TriedIPs: tried}
		}
		return bulkHostOutcome{Status: bulkHostSuccess, ConnectIP: connectIP, TriedIPs: tried}
	})
}
