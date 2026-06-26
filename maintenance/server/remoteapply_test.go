package server

import (
	"os"
	"path/filepath"
	"testing"

	"contrabass-agent/maintenance/appmeta"
)

func dualAgentPreparedBundle(t *testing.T, workDir, bundleContent string) *PreparedBundle {
	t.Helper()
	bundlePath := filepath.Join(workDir, "upload.bundle.tar.gz")
	if err := os.WriteFile(bundlePath, []byte(bundleContent), 0644); err != nil {
		t.Fatal(err)
	}
	controlPath := filepath.Join(workDir, "control")
	computePath := filepath.Join(workDir, "compute")
	for _, p := range []string{controlPath, computePath} {
		if err := os.WriteFile(p, []byte("bin"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return &PreparedBundle{
		VersionKey:     "1.0.0-1",
		ConfigData:     []byte("key: val\n"),
		ConfigFileName: appmeta.ConfigFileName,
		Agents: []PreparedBundleAgent{
			{DestBasename: appmeta.BundleAgentControlName, ExtractPath: controlPath},
			{DestBasename: appmeta.BundleAgentComputeName, ExtractPath: computePath},
		},
		BundlePath: bundlePath,
		WorkDir:    workDir,
	}
}

func TestStagePreparedBundleForRemoteApply_preservesBundleTarWhenNotReuse(t *testing.T) {
	pb := dualAgentPreparedBundle(t, t.TempDir(), "tar-bytes")
	s := &Server{}
	dir, err := s.stagePreparedBundleForRemoteApply(pb, false)
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, StagedBundleFileName)
	data, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "tar-bytes" {
		t.Fatalf("staged bundle = %q", data)
	}
}

func TestStagePreparedBundleForRemoteApply_skipsBundleTarWhenReuse(t *testing.T) {
	pb := dualAgentPreparedBundle(t, t.TempDir(), "tar-bytes")
	s := &Server{}
	dir, err := s.stagePreparedBundleForRemoteApply(pb, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, StagedBundleFileName)); !os.IsNotExist(err) {
		t.Fatalf("StagedBundleFileName should be absent when reuse=true, err=%v", err)
	}
}
