package replcli

import (
	"testing"

	"contrabass-agent/maintenance/clirest"
)

func TestReplCompleter_commands(t *testing.T) {
	c := &replCompleter{}
	got := c.candidates(nil, "hos")
	if len(got) == 0 || got[0] != "host-info" {
		t.Fatalf("candidates = %v", got)
	}
}

func TestReplCompleter_helpTopics(t *testing.T) {
	c := &replCompleter{}
	got := c.candidates([]string{"help"}, "disc")
	if len(got) != 1 || got[0] != "discover" {
		t.Fatalf("candidates = %v", got)
	}
}

func TestReplCompleter_cachedTargets(t *testing.T) {
	c := &replCompleter{session: &Session{
		CachedHosts: []clirest.BulkPushHost{
			{PrimaryIP: "10.0.0.2", IPs: []string{"10.0.0.2", "10.0.0.3"}},
		},
	}}
	got := c.candidates([]string{"host-info"}, "10")
	want := []string{"10.0.0.2", "10.0.0.3"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v", got)
		}
	}
}

func TestFormatCompleterSuffixes(t *testing.T) {
	suffixes, n := formatCompleterSuffixes("hos", []string{"host-info", "hosts"})
	if n != 3 || len(suffixes) != 2 {
		t.Fatalf("n=%d suffixes=%v", n, suffixes)
	}
}
