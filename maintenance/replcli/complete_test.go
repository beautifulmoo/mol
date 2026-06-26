package replcli

import (
	"os"
	"path/filepath"
	"strings"
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
	if len(got) != 2 || got[0] != "discover" || got[1] != "discovery" {
		t.Fatalf("candidates = %v", got)
	}
}

func TestReplCompleter_discoverAlias(t *testing.T) {
	c := &replCompleter{}
	got := c.candidates(nil, "discover")
	wantDiscover := false
	for _, name := range got {
		if name == "discover" {
			wantDiscover = true
		}
	}
	if !wantDiscover {
		t.Fatalf("discover missing from candidates = %v", got)
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

func TestCompletingBundlePath(t *testing.T) {
	if !completingBundlePath([]string{"apply-update", "self"}) {
		t.Fatal("expected bundle completion after target")
	}
	if completingBundlePath([]string{"apply-update"}) {
		t.Fatal("target not set yet")
	}
	if !completingBundlePath([]string{"apply-update", "-use-bundle-config", "10.0.0.1"}) {
		t.Fatal("expected bundle completion with flags")
	}
	if completingBundlePath([]string{"apply-update", "self", "bundle.tar.gz"}) {
		t.Fatal("bundle already complete")
	}
}

func TestCompletingApplyUpdateAllBundle(t *testing.T) {
	if !completingApplyUpdateAllBundle([]string{"apply-update-all"}) {
		t.Fatal("expected bundle completion")
	}
	if !completingApplyUpdateAllBundle([]string{"apply-update-all", "-use-bundle-config"}) {
		t.Fatal("expected bundle completion with flags only")
	}
	if completingApplyUpdateAllBundle([]string{"apply-update-all", "bundle.tar.gz"}) {
		t.Fatal("bundle already set")
	}
}

func TestReplCompleter_applyUpdateBundlePath(t *testing.T) {
	c := &replCompleter{}
	got := c.candidates([]string{"apply-update", "self"}, "go.")
	if len(got) == 0 {
		t.Skip("no files in cwd matching go.")
	}
	for _, p := range got {
		if !strings.HasPrefix(strings.ToLower(p), "go.") {
			t.Fatalf("candidate %q", p)
		}
	}
}

func TestFilePathCompletionContext(t *testing.T) {
	dir, base, prefix := filePathCompletionContext("/tmp/te")
	if base != "te" || !strings.HasSuffix(dir, "tmp") {
		t.Fatalf("dir=%q base=%q", dir, base)
	}
	if prefix != "/tmp/" && !strings.HasPrefix(prefix, "/tmp/") {
		t.Fatalf("prefix=%q", prefix)
	}
	_, _, pfx := filePathCompletionContext("/tmp/")
	if pfx != "/tmp/" {
		t.Fatalf("trailing slash prefix=%q", pfx)
	}
}

func TestFilePathCandidates_relativeDist(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "bundle.tar.gz"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	got := filePathCandidates("./dist/")
	if len(got) == 0 {
		t.Fatalf("expected matches under ./dist/, got %v", got)
	}
	found := false
	for _, p := range got {
		if strings.HasSuffix(p, "bundle.tar.gz") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("candidates=%v", got)
	}

	suffixes, n := formatCompleterSuffixes("./di", []string{"./dist/"})
	if n != 4 || len(suffixes) != 1 || string(suffixes[0]) != "st/" {
		t.Fatalf("suffixes=%v n=%d", suffixes, n)
	}
}
