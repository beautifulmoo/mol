package versionsapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyPreviousConfigToVersion(t *testing.T) {
	root := t.TempDir()
	oldPrevious := filepath.Join(root, "versions", "1.0.0")
	currentVer := filepath.Join(root, "versions", "2.0.0")
	newVer := filepath.Join(root, "versions", "3.0.0")
	for _, d := range []string{oldPrevious, currentVer, newVer} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldPrevious, "agent.local.yml"), []byte("db: old-previous\n"), 0644); err != nil {
		t.Fatal(err)
	}
	currentCfg := "db: pre-update-current\n"
	if err := os.WriteFile(filepath.Join(currentVer, "agent.local.yml"), []byte(currentCfg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newVer, "agent.local.yml"), []byte("db: bundle-default\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/1.0.0", filepath.Join(root, "previous")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/2.0.0", filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}

	if err := CopyPreviousConfigToVersion(root, "", "", newVer); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(newVer, "agent.local.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != currentCfg {
		t.Fatalf("config = %q, want %q (must copy pre-update current, not previous symlink)", got, currentCfg)
	}
}

func TestCopyPreviousConfigToVersionUsesVersionsBaseCurrent(t *testing.T) {
	root := t.TempDir()
	deployRoot := filepath.Join(root, "deploy")
	installRoot := filepath.Join(root, "install")
	currentVer := filepath.Join(installRoot, "versions", "2.0.0")
	newVer := filepath.Join(installRoot, "versions", "3.0.0")
	for _, d := range []string{deployRoot, currentVer, newVer} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	currentCfg := "db: install-prefix-current\n"
	if err := os.WriteFile(filepath.Join(currentVer, "agent.local.yml"), []byte(currentCfg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newVer, "agent.local.yml"), []byte("db: bundle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("versions/2.0.0", filepath.Join(installRoot, "current")); err != nil {
		t.Fatal(err)
	}

	if err := CopyPreviousConfigToVersion(deployRoot, installRoot, "", newVer); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(newVer, "agent.local.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != currentCfg {
		t.Fatalf("config = %q, want %q", got, currentCfg)
	}
}

func TestCopyPreviousConfigToVersionSkipsMissingCurrent(t *testing.T) {
	root := t.TempDir()
	newVer := filepath.Join(root, "versions", "2.0.0")
	if err := os.MkdirAll(newVer, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newVer, "agent.local.yml"), []byte("unchanged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CopyPreviousConfigToVersion(root, "", "", newVer); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(newVer, "agent.local.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "unchanged\n" {
		t.Fatalf("config = %q, want unchanged", got)
	}
}
