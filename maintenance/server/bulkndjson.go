package server

import (
	"log"
	"net/http"

	"contrabass-agent/maintenance/server/remoteregistry"
)

type bulkHostStatus string

const (
	bulkHostSuccess bulkHostStatus = "success"
	bulkHostFail    bulkHostStatus = "fail"
	bulkHostSkipped bulkHostStatus = "skipped"
)

type bulkHostOutcome struct {
	Status               bulkHostStatus
	Message              string
	ConnectIP            string
	TriedIPs             []string
	Extra                map[string]interface{}
	ContinueOnWriteError bool
}

type bulkRunSummary struct {
	Total, Succeeded, Failed, Skipped int
}

type bulkNDJSONOptions struct {
	StartExtra map[string]interface{}
	DoneExtra  func(bulkRunSummary) map[string]interface{}
	HistoryFmt func(bulkRunSummary) string
}

type bulkHostStep func(remote remoteregistry.Remote, base map[string]interface{}) bulkHostOutcome

func beginBulkNDJSON(w http.ResponseWriter) http.Flusher {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	return flusher
}

func bulkProgressBase(remote remoteregistry.Remote, index, total int) map[string]interface{} {
	return map[string]interface{}{
		"type":     "progress",
		"current":  index + 1,
		"total":    total,
		"ip":       bulkDisplayIP(remote),
		"hostname": remote.Hostname,
		"cpu_uuid": remote.CPUUUID,
	}
}

func applyBulkHostOutcome(evt map[string]interface{}, outcome bulkHostOutcome) {
	if len(outcome.TriedIPs) > 0 {
		evt["tried_ips"] = outcome.TriedIPs
	}
	for k, v := range outcome.Extra {
		evt[k] = v
	}
	switch outcome.Status {
	case bulkHostSuccess:
		evt["status"] = "success"
		if outcome.ConnectIP != "" {
			evt["connect_ip"] = outcome.ConnectIP
		}
	case bulkHostSkipped:
		evt["status"] = "skipped"
		if outcome.Message != "" {
			evt["message"] = outcome.Message
		}
	default:
		evt["status"] = "fail"
		if outcome.Message != "" {
			evt["message"] = outcome.Message
		}
	}
}

func (s *Server) runBulkRemoteNDJSON(w http.ResponseWriter, remotes []remoteregistry.Remote, opts bulkNDJSONOptions, step bulkHostStep) bulkRunSummary {
	flusher := beginBulkNDJSON(w)
	total := len(remotes)
	start := map[string]interface{}{
		"type":  "start",
		"total": total,
	}
	for k, v := range opts.StartExtra {
		start[k] = v
	}
	if err := writeNDJSONLine(w, flusher, start); err != nil {
		return bulkRunSummary{Total: total}
	}

	var summary bulkRunSummary
	summary.Total = total
	for i, remote := range remotes {
		evt := bulkProgressBase(remote, i, total)
		outcome := step(remote, evt)
		applyBulkHostOutcome(evt, outcome)
		switch outcome.Status {
		case bulkHostSuccess:
			summary.Succeeded++
		case bulkHostSkipped:
			summary.Skipped++
		default:
			summary.Failed++
		}
		if err := writeNDJSONLine(w, flusher, evt); err != nil && !outcome.ContinueOnWriteError {
			return summary
		}
	}
	if opts.HistoryFmt != nil {
		if err := s.appendDeployHistory(opts.HistoryFmt(summary)); err != nil {
			log.Printf("update_history: bulk finish: %v", err)
		}
	}
	done := map[string]interface{}{
		"type":      "done",
		"total":     total,
		"succeeded": summary.Succeeded,
		"failed":    summary.Failed,
	}
	if opts.DoneExtra != nil {
		for k, v := range opts.DoneExtra(summary) {
			done[k] = v
		}
	}
	_ = writeNDJSONLine(w, flusher, done)
	return summary
}
