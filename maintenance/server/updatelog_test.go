package server

import (
	"os"
	"strings"
	"testing"
)

func TestAppendDeployHistory(t *testing.T) {
	dir := t.TempDir()
	if err := appendDeployHistory(dir, "config push-all started (1 host(s))"); err != nil {
		t.Fatal(err)
	}
	if err := appendDeployHistory(dir, "config push-all host vm (10.0.0.1) success"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(deployHistoryPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2:\n%s", len(lines), string(data))
	}
	if !strings.Contains(lines[0], "config push-all started") {
		t.Fatalf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "success") {
		t.Fatalf("second line = %q", lines[1])
	}
}

func TestHistoryLineIndicatesRecentRollback(t *testing.T) {
	noWarn := []string{
		"[2026-06-11 18:29:57] service restart-all finished succeeded=1 failed=0",
		"[2026-06-11 19:00:00] apply-update-all finished succeeded=2 failed=0 skipped=1",
		"[2026-06-11 19:01:00] rollback-all finished succeeded=1 failed=0 skipped=2",
		"[2026-06-11 16:33:04] config push-all finished succeeded=1 failed=0",
		"[2026-06-11 16:21:17] config push-all finished succeeded=2 failed=0",
		"[2026-06-11 12:00:00] update 1.2.3 success",
		"[2026-06-11 12:00:00] rollback success",
	}
	for _, line := range noWarn {
		if historyLineIndicatesRecentRollback(line) {
			t.Fatalf("expected no warning for %q", line)
		}
	}
	warn := []string{
		"[2026-06-11 12:00:00] update 1.2.3 failed: service did not stop",
		"[2026-06-11 12:00:00] update 1.2.3 failed (health), invoking rollback",
		"[2026-06-11 12:00:00] rollback failed: service did not start",
		"[2026-06-11 12:00:00] rollback completed after update failure",
	}
	for _, line := range warn {
		if !historyLineIndicatesRecentRollback(line) {
			t.Fatalf("expected warning for %q", line)
		}
	}
}

func TestNormalizeUpdateLogAPIPayloadBulkSummaryNoRollbackWarning(t *testing.T) {
	raw := strings.Join([]string{
		"[2026-06-11 16:21:17] config push-all finished succeeded=2 failed=0",
		"[2026-06-11 16:33:04] config push-all finished succeeded=1 failed=0",
		"[2026-06-11 18:29:57] service restart-all finished succeeded=1 failed=0",
	}, "\n") + "\n"
	payload := normalizeUpdateLogAPIPayload(raw, false)
	if payload["recent_rollback"] != false {
		t.Fatalf("recent_rollback = %v, want false", payload["recent_rollback"])
	}
}
