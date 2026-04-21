package versionsapi

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"contrabass-agent/internal/config"
	"contrabass-agent/internal/updatescripts"
	"contrabass-agent/maintenance/appmeta"
)

// stagedBundleFileName matches server/bundleupload.StagedBundleFileName (removed from versions/ after copy).
const stagedBundleFileName = "upload.bundle.tar.gz"

// DeployRootFromConfig returns the deploy filesystem root (staging/, current/), matching the server's
// normalized local `base` for apply-update and versions/switch-current.
func DeployRootFromConfig(cfg *config.Config) string {
	if cfg == nil {
		return "/var/lib/contrabass/mole"
	}
	d := strings.TrimSpace(strings.TrimSuffix(cfg.DeployBase, "/"))
	if d == "" {
		return "/var/lib/contrabass/mole"
	}
	return d
}

// RunSwitchCurrentWithRoots performs the same steps as POST …/versions/switch-current for local (no ip / self):
// resolve staging vs versions/, copy staging into versions/ if needed, write embedded scripts under deployRoot/current,
// then systemd-run update.sh. deployRoot must be the normalized deploy base; installPrefix and deployBaseRaw are raw
// YAML fields used with VersionsBaseFromParts for the versions/ tree.
func RunSwitchCurrentWithRoots(deployRoot string, installPrefix, deployBaseRaw, version string) error {
	if err := config.ValidateVersionKeyPath(version); err != nil {
		return err
	}
	vb := VersionsBaseFromParts(installPrefix, deployBaseRaw)
	versionDir, fromStaging := resolveVersionDirForSwitch(deployRoot, vb, version)
	if versionDir == "" {
		return fmt.Errorf("해당 버전이 스테이징 또는 versions에 없습니다: %s", version)
	}
	if fromStaging {
		if err := copyStagingToVersionsDir(deployRoot, vb, version); err != nil {
			return fmt.Errorf("스테이징→versions 복사 실패: %w", err)
		}
	}
	currentPath := filepath.Join(deployRoot, "current")
	if _, err := os.Stat(currentPath); err != nil {
		return fmt.Errorf("배포 루트에 current가 없습니다. 업데이트를 적용할 수 없습니다: %s", currentPath)
	}
	updateScript := filepath.Join(currentPath, "update.sh")
	rollbackScript := filepath.Join(currentPath, "rollback.sh")
	if err := os.WriteFile(updateScript, []byte(updatescripts.UpdateSh), 0755); err != nil {
		return fmt.Errorf("update.sh: %w%s", err, hintIfPermissionDenied(err))
	}
	if err := os.WriteFile(rollbackScript, []byte(updatescripts.RollbackSh), 0755); err != nil {
		_ = os.Remove(updateScript)
		return fmt.Errorf("rollback.sh: %w%s", err, hintIfPermissionDenied(err))
	}
	exec.Command("systemctl", "reset-failed", appmeta.UpdateTransientUnit).Run()
	exec.Command("systemctl", "stop", appmeta.UpdateTransientUnit).Run()
	cmd := exec.Command("systemd-run",
		"--unit="+appmeta.UpdateTransientUnitStem,
		"--property=RemainAfterExit=yes",
		"/bin/bash", updateScript, version)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		_ = os.Remove(updateScript)
		_ = os.Remove(rollbackScript)
		return fmt.Errorf("systemd-run(update.sh) 실패: %w", err)
	}
	log.Printf("RunSwitchCurrentWithRoots: systemd-run --unit=%s /bin/bash %s %s", appmeta.UpdateTransientUnitStem, updateScript, version)
	return nil
}

func hintIfPermissionDenied(err error) string {
	if err != nil && os.IsPermission(err) {
		return " (need write access under DeployBase/current; run with sudo or as the directory owner)"
	}
	return ""
}

func resolveVersionDirForSwitch(deployRoot, versionsBaseRoot, version string) (dir string, fromStaging bool) {
	stg := filepath.Join(deployRoot, "staging", version)
	if dirHasAgentBinary(stg) {
		return stg, true
	}
	ver := filepath.Join(versionsBaseRoot, "versions", version)
	if dirHasAgentBinary(ver) {
		return ver, false
	}
	return "", false
}

func copyStagingToVersionsDir(deployRoot, versionsBaseRoot, version string) error {
	stg := filepath.Join(deployRoot, "staging", version)
	ver := filepath.Join(versionsBaseRoot, "versions", version)
	if _, err := os.Stat(stg); err != nil {
		return fmt.Errorf("스테이징 디렉터리: %w", err)
	}
	if err := os.RemoveAll(ver); err != nil {
		return err
	}
	if err := os.MkdirAll(ver, 0755); err != nil {
		return err
	}
	if err := copyStagingTreeInto(stg, ver); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(ver, stagedBundleFileName))
	return nil
}

func copyStagingTreeInto(stg, ver string) error {
	return filepath.WalkDir(stg, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(stg, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(ver, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0644
		}
		return copyFileRobust(path, dst, mode)
	})
}

func copyFileRobust(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}
