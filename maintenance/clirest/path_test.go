package clirest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandLocalPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	got, err := ExpandLocalPath("~/work/mol")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(home, "work/mol"))
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got, err = ExpandLocalPath("./dist/foo.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean("./dist/foo.tar.gz") {
		t.Fatalf("got %q", got)
	}
}
