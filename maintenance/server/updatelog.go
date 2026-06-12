package server

import (
	"fmt"
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
