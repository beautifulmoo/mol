package replcli

import (
	"strings"
	"testing"

	"contrabass-agent/maintenance/appmeta"
)

func TestReplHistoryPath_underUserCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := replHistoryPath()
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.Contains(path, appmeta.BinaryName) {
		t.Fatalf("path = %q, want binary name segment", path)
	}
	if !strings.HasSuffix(path, "repl_history") {
		t.Fatalf("path = %q", path)
	}
}
