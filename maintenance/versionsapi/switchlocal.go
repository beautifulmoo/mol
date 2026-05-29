package versionsapi

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/updatescripts"
)

// stagedBundleFileName matches server/bundleupload.StagedBundleFileName (removed from versions/ after copy).
const stagedBundleFileName = "upload.bundle.tar.gz"

// DeployRootFromConfig returns the deploy filesystem root (staging/, current/), matching the server's
// normalized local `base` for apply-update and versions/switch-current.
func DeployRootFromConfig(cfg *agentcfg.Config) string {
	if cfg == nil {
		return "/var/lib/contrabass/mole"
	}
	d := strings.TrimSpace(strings.TrimSuffix(cfg.DeployBase, "/"))
	if d == "" {
		return "/var/lib/contrabass/mole"
	}
	return d
}

// RunSwitchCurrentWithRoots is the local path for apply-update and versions/switch-current (no remote HTTP).
func RunSwitchCurrentWithRoots(deployRoot, installPrefix, deployBaseRaw, version string) error {
	return RunSwitchCurrentWithRootsVariant(deployRoot, installPrefix, deployBaseRaw, version, "", "")
}

// RunSwitchCurrentWithRootsVariant is like RunSwitchCurrentWithRoots with explicit agent variant and ldflags fallback.
func RunSwitchCurrentWithRootsVariant(deployRoot, installPrefix, deployBaseRaw, version, agentVariant, buildVariantFallback string) error {
	if err := prepareVersionForUpdate(deployRoot, installPrefix, deployBaseRaw, version, agentVariant, buildVariantFallback); err != nil {
		return err
	}
	return runEmbeddedUpdate(deployRoot, version)
}

// MaterializeCanonicalAgent copies the selected bundle agent (control or compute) to BinaryName in versionDir.
// Legacy trees with only BinaryName and no dual agents are left unchanged.
func MaterializeCanonicalAgent(versionDir, agentVariant string) error {
	variant, err := appmeta.ParseAgentVariant(agentVariant)
	if err != nil {
		return err
	}
	if versionDir == "" {
		return fmt.Errorf("version directory is empty")
	}
	if !StagingHasDualAgents(versionDir) {
		if !dirHasAgentBinary(versionDir) {
			return fmt.Errorf("버전 디렉터리에 %s 또는 %s/%s 가 없습니다",
				appmeta.BinaryName, appmeta.BundleAgentControlName, appmeta.BundleAgentComputeName)
		}
		return nil
	}
	srcName := appmeta.BundleAgentBasenameForVariant(variant)
	src := filepath.Join(versionDir, srcName)
	dst := filepath.Join(versionDir, appmeta.BinaryName)
	if err := copyFileRobust(src, dst, 0755); err != nil {
		return fmt.Errorf("copy %s → %s: %w", srcName, appmeta.BinaryName, err)
	}
	return nil
}

// InstalledBuildVariantFromDeploy reads build_variant from deployRoot/current/contrabass-moleU --version.
func InstalledBuildVariantFromDeploy(deployRoot string) string {
	deployRoot = strings.TrimSuffix(strings.TrimSpace(deployRoot), "/")
	if deployRoot == "" {
		return ""
	}
	bin := filepath.Join(deployRoot, "current", appmeta.BinaryName)
	line, err := AgentVersionLine(bin)
	if err != nil {
		return ""
	}
	return appmeta.ParseBuildVariantFromVersionLine(line)
}

// AgentVersionLine runs binPath --version, or on failure agent --version.
func AgentVersionLine(binPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	try := func(argv ...string) (string, error) {
		cmd := exec.CommandContext(ctx, binPath, argv...)
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		line := strings.TrimSpace(string(out))
		if line == "" {
			return "", fmt.Errorf("empty version output")
		}
		return line, nil
	}
	if line, err := try("--version"); err == nil {
		return line, nil
	}
	return try("agent", "--version")
}

func prepareVersionForUpdate(deployRoot, installPrefix, deployBaseRaw, version, agentVariantExplicit, buildVariantFallback string) error {
	if err := agentcfg.ValidateVersionKeyPath(version); err != nil {
		return err
	}
	vb := VersionsBaseFromParts(installPrefix, deployBaseRaw)
	versionDir, fromStaging := resolveVersionDirForSwitch(deployRoot, vb, version)
	if versionDir == "" {
		return fmt.Errorf("해당 버전이 스테이징 또는 versions에 없습니다: %s", version)
	}
	targetDir := filepath.Join(vb, "versions", version)
	if fromStaging {
		if err := copyStagingToVersionsDir(deployRoot, vb, version); err != nil {
			return fmt.Errorf("스테이징→versions 복사 실패: %w", err)
		}
	} else {
		targetDir = versionDir
	}
	instBV := InstalledBuildVariantFromDeploy(deployRoot)
	if instBV == "" {
		instBV = strings.TrimSpace(buildVariantFallback)
	}
	variant, err := appmeta.ResolveAgentVariantForApply(agentVariantExplicit, instBV)
	if err != nil {
		return err
	}
	if err := MaterializeCanonicalAgent(targetDir, variant); err != nil {
		return fmt.Errorf("실행 파일 준비: %w", err)
	}
	return nil
}

func runEmbeddedUpdate(deployRoot, version string) error {
	deployRoot = strings.TrimSuffix(strings.TrimSpace(deployRoot), "/")
	if deployRoot == "" {
		deployRoot = "/var/lib/contrabass/mole"
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
	if DirHasStagedAgents(stg) {
		return stg, true
	}
	ver := filepath.Join(versionsBaseRoot, "versions", version)
	if DirHasStagedAgents(ver) {
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
