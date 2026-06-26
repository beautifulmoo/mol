package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var updateHistoryTimestampPrefix = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\] `)

const updateHistoryFlockWait = 30 * time.Second

func deployHistoryPath(deployBase string) string {
	base := strings.TrimSuffix(deployBase, "/")
	if base == "" {
		base = "/var/lib/contrabass/mole"
	}
	return filepath.Join(base, "update_history.log")
}

// appendDeployHistory appends one line to {DeployBase}/update_history.log with flock (same policy as embedded update.sh).
func appendDeployHistory(deployBase, message string) error {
	historyPath := deployHistoryPath(deployBase)
	lockPath := historyPath + ".lock"
	line := fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), strings.TrimSpace(message))
	if line == "" || strings.HasSuffix(line, "] ") {
		return fmt.Errorf("empty history message")
	}

	if err := os.MkdirAll(filepath.Dir(historyPath), 0755); err != nil && !os.IsExist(err) {
		return err
	}

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer lockFile.Close()

	deadline := time.Now().Add(updateHistoryFlockWait)
	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("update_history.log flock timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

func (s *Server) appendDeployHistory(message string) error {
	return appendDeployHistory(s.deployBase, message)
}

func updateHistoryMessage(line string) string {
	line = strings.TrimSpace(line)
	if m := updateHistoryTimestampPrefix.FindString(line); m != "" {
		return strings.TrimSpace(line[len(m):])
	}
	return line
}

// historyLineIndicatesRecentRollback reports whether the last history line means a failed update or rollback.
// Bulk config push / service restart summaries (failed=N counts) are not update failures.
func historyLineIndicatesRecentRollback(line string) bool {
	msg := strings.ToLower(updateHistoryMessage(line))
	if msg == "" {
		return false
	}
	switch {
	case strings.HasPrefix(msg, "config push-all finished "),
		strings.HasPrefix(msg, "service restart-all finished "),
		strings.HasPrefix(msg, "apply-update-all finished "),
		strings.HasPrefix(msg, "rollback-all finished "):
		return false
	case strings.HasPrefix(msg, "update ") && strings.Contains(msg, " success"):
		return false
	case strings.HasPrefix(msg, "rollback success"):
		return false
	}
	if strings.Contains(msg, "rollback failed") {
		return true
	}
	if strings.Contains(msg, "rollback completed after update failure") {
		return true
	}
	if strings.HasPrefix(msg, "update ") && strings.Contains(msg, " failed") {
		return true
	}
	return false
}

func (s *Server) handleUpdateLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip != "" && ip != "self" {
		out, err := s.fetchRemoteAPI(ip, http.MethodGet, "/update-log", nil)
		if err != nil {
			sendRemoteAPIError(w, s.send, "remote update-log request failed: ", err)
			return
		}
		if out.Status == "success" {
			payload := normalizeUpdateLogAPIPayload(out.Data, false)
			w.Header().Set("Cache-Control", "no-store")
			s.send(w, "success", payload, http.StatusOK)
			return
		}
		s.send(w, out.Status, out.Data, http.StatusOK)
		return
	}
	base := s.deployBaseOrDefault()
	historyPath := filepath.Join(base, "update_history.log")
	payload, err := updateLogPayloadFromFile(historyPath, isUpdateUnitActive())
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Cache-Control", "no-store")
			s.send(w, "success", map[string]interface{}{"output": "(no entries yet)", "recent_rollback": false}, http.StatusOK)
			return
		}
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.send(w, "success", payload, http.StatusOK)
}

const updateLogMaxLines = 10

func updateLogPayloadFromFile(historyPath string, updateInProgress bool) (map[string]interface{}, error) {
	data, err := os.ReadFile(historyPath)
	if err != nil {
		return nil, err
	}
	return normalizeUpdateLogAPIPayload(string(data), updateInProgress), nil
}

// normalizeUpdateLogAPIPayload returns tail lines oldest-first for the UI to reverse (newest on top).
// data may be raw file bytes/string or a proxied API map with an "output" field.
func normalizeUpdateLogAPIPayload(data interface{}, updateInProgress bool) map[string]interface{} {
	raw := ""
	recentRollback := false
	switch v := data.(type) {
	case string:
		raw = v
	case map[string]interface{}:
		if o, ok := v["output"].(string); ok {
			raw = o
		}
		if rb, ok := v["recent_rollback"].(bool); ok {
			recentRollback = rb
		}
	case []byte:
		raw = string(v)
	}
	lines := splitUpdateLogLines(raw)
	if len(lines) == 0 {
		return map[string]interface{}{"output": "(no entries yet)", "recent_rollback": false}
	}
	outLines := lines
	if len(lines) > updateLogMaxLines {
		outLines = lines[len(lines)-updateLogMaxLines:]
	}
	output := strings.Join(outLines, "\n")
	if len(lines) > 0 {
		recentRollback = historyLineIndicatesRecentRollback(lines[len(lines)-1])
	}
	if recentRollback && updateInProgress {
		recentRollback = false
	}
	return map[string]interface{}{"output": output, "recent_rollback": recentRollback}
}

func splitUpdateLogLines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "(no entries yet)" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
