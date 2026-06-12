package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCurrentConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.local.yml")
	if err := os.WriteFile(cfg, []byte("key: old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := backupCurrentConfigFile(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, ConfigBackupBasename))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "key: old\n" {
		t.Fatalf("backup content = %q", string(got))
	}
}

func TestBackupCurrentConfigFile_missingSource(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.local.yml")
	if err := backupCurrentConfigFile(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ConfigBackupBasename)); !os.IsNotExist(err) {
		t.Fatalf("expected no backup file, stat err=%v", err)
	}
}

func TestSaveCurrentConfigContent_withBackup(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "agent.local.yml")
	if err := os.WriteFile(cfg, []byte("key: old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	content := "Server:\n  HTTPPort: 8888\nMaintenance:\n  MaintenancePort: 8889\n"
	if err := saveCurrentConfigContent(cfg, content, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("config = %q", string(got))
	}
	backup, err := os.ReadFile(filepath.Join(dir, ConfigBackupBasename))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "key: old\n" {
		t.Fatalf("backup = %q", string(backup))
	}
}
