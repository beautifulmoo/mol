package server

import (
	"archive/tar"
	"compress/gzip"
	"contrabass-agent/maintenance/agentcfg"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/versionsapi"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	bundleManifestName = "contrabass.manifest.yaml"
	uploadBundleField  = "bundle"
	bundleManifestV2   = 2
	bundleManifestV1   = 1
)

// StagedBundleFileName is the original upload tar.gz kept next to the extracted agent and config
// under staging/<version>/ and versions/<version>/ so remote POST /upload can re-send the same bytes
// without rebuilding the archive (manifest may list arbitrary future files).
const StagedBundleFileName = "upload.bundle.tar.gz"

// maxBundleMembers limits entries processed from a tar.gz (defense in depth).
const maxBundleMembers = 512

type bundleFileRef struct {
	Path   string `yaml:"path"`
	Sha256 string `yaml:"sha256"`
}

// bundleManifestDoc matches maintenance/packaging/contrabass.manifest.yaml.
// manifestVersion 1: agent + config. manifestVersion 2: agent_control + agent_compute + config.
type bundleManifestDoc struct {
	ManifestVersion int           `yaml:"manifestVersion"`
	Agent           bundleFileRef `yaml:"agent"`
	AgentControl    bundleFileRef `yaml:"agent_control"`
	AgentCompute    bundleFileRef `yaml:"agent_compute"`
	Config          bundleFileRef `yaml:"config"`
}

// PreparedBundleAgent is one agent binary extracted from a bundle and staged under DestBasename.
type PreparedBundleAgent struct {
	DestBasename string
	ExtractPath  string
}

// PreparedBundle is the result of validating and extracting an upload tar.gz.
type PreparedBundle struct {
	VersionKey     string
	ConfigData     []byte
	ConfigFileName string
	Agents         []PreparedBundleAgent
	BundlePath     string
	WorkDir        string
}

func normalizeBundlePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(filepath.ToSlash(p), "/")
	return filepath.ToSlash(p)
}

func bundleMemberAbs(root, manifestPath string) (string, error) {
	rel := normalizeBundlePath(manifestPath)
	if rel == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return "", errors.New("path not allowed (.. or absolute)")
	}
	dest := filepath.Join(root, filepath.FromSlash(rel))
	cr, err := filepath.Rel(root, dest)
	if err != nil || strings.HasPrefix(cr, "..") {
		return "", errors.New("path escapes archive root")
	}
	return dest, nil
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Matches(expectedHex, actualHex string) bool {
	e := strings.ToLower(strings.TrimSpace(expectedHex))
	if e == "" {
		return true
	}
	a := strings.ToLower(strings.TrimSpace(actualHex))
	return len(a) == 64 && e == a
}

func verifyBundleFileHash(absPath string, ref bundleFileRef, label string) error {
	if strings.TrimSpace(ref.Path) == "" {
		return fmt.Errorf("manifest missing %s.path", label)
	}
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf("%s file missing: %s", label, ref.Path)
	}
	h, err := fileSHA256Hex(absPath)
	if err != nil {
		return fmt.Errorf("%s hash: %w", label, err)
	}
	if !sha256Matches(ref.Sha256, h) {
		return fmt.Errorf("%s sha256 mismatch (manifest vs file)", label)
	}
	return nil
}

func basenameFromManifestPath(manifestPath string) (string, error) {
	name := path.Base(normalizeBundlePath(manifestPath))
	if strings.TrimSpace(name) == "" || name == "." || strings.Contains(name, "/") {
		return "", fmt.Errorf("invalid path basename: %q", manifestPath)
	}
	return name, nil
}

// extractTarGzSafe unpacks r into rootDir. Total uncompressed size must not exceed maxBytes.
func extractTarGzSafe(r io.Reader, rootDir string, maxBytes int64) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var total int64
	var nmembers int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		nmembers++
		if nmembers > maxBundleMembers {
			return errors.New("too many tar entries")
		}
		name := hdr.Name
		switch hdr.Typeflag {
		case tar.TypeDir:
			rel := normalizeBundlePath(name)
			if rel == "" || rel == "." {
				continue
			}
			dest, err := bundleMemberAbs(rootDir, name)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
			continue
		case tar.TypeReg, tar.TypeRegA:
			if hdr.Size < 0 || hdr.Size > maxBytes {
				return errors.New("invalid tar entry size")
			}
			if total+hdr.Size > maxBytes {
				return errors.New("uncompressed total exceeds limit")
			}
			dest, err := bundleMemberAbs(rootDir, name)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode&0777))
			if err != nil {
				return err
			}
			nw, err := io.Copy(f, io.LimitReader(tr, hdr.Size))
			f.Close()
			if err != nil {
				return err
			}
			if nw != hdr.Size {
				return errors.New("tar entry size mismatch")
			}
			total += hdr.Size
		case tar.TypeSymlink, tar.TypeLink:
			return errors.New("symlinks and hard links are not allowed")
		default:
			return fmt.Errorf("unsupported tar entry type: %v", hdr.Typeflag)
		}
	}
	return nil
}

func parseBundleManifest(data []byte) (*bundleManifestDoc, error) {
	var m bundleManifestDoc
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest YAML: %w", err)
	}
	switch m.ManifestVersion {
	case bundleManifestV2:
		if strings.TrimSpace(m.AgentControl.Path) == "" || strings.TrimSpace(m.AgentCompute.Path) == "" {
			return nil, errors.New("manifestVersion 2 requires agent_control.path and agent_compute.path")
		}
	case bundleManifestV1:
		if strings.TrimSpace(m.Agent.Path) == "" {
			return nil, errors.New("manifest missing agent.path")
		}
	default:
		return nil, fmt.Errorf("manifestVersion %d not supported (use %d or %d)", m.ManifestVersion, bundleManifestV1, bundleManifestV2)
	}
	if strings.TrimSpace(m.Config.Path) == "" {
		return nil, errors.New("manifest missing config.path")
	}
	return &m, nil
}

func validateAgentELF(agentPath string) error {
	hdr := make([]byte, 4)
	af, err := os.Open(agentPath)
	if err != nil {
		return err
	}
	_, err = io.ReadFull(af, hdr)
	_ = af.Close()
	if err != nil {
		return fmt.Errorf("executable too short")
	}
	if !isELFExecutable(hdr) {
		return fmt.Errorf("not a valid ELF executable")
	}
	return nil
}

func agentRefsFromManifest(m *bundleManifestDoc) ([]bundleFileRef, error) {
	switch m.ManifestVersion {
	case bundleManifestV2:
		return []bundleFileRef{m.AgentControl, m.AgentCompute}, nil
	case bundleManifestV1:
		return []bundleFileRef{m.Agent}, nil
	default:
		return nil, fmt.Errorf("unsupported manifestVersion %d", m.ManifestVersion)
	}
}

// writeBundleTarGz writes manifest v2 + control + compute + config.
func writeBundleTarGz(w io.Writer, controlPath, computePath, configPath string) error {
	ch, err := fileSHA256Hex(controlPath)
	if err != nil {
		return err
	}
	coh, err := fileSHA256Hex(computePath)
	if err != nil {
		return err
	}
	cfh, err := fileSHA256Hex(configPath)
	if err != nil {
		return err
	}
	controlName := filepath.Base(controlPath)
	computeName := filepath.Base(computePath)
	configName := filepath.Base(configPath)
	manifestBody := fmt.Sprintf(`manifestVersion: 2

bundle:
  format: tar.gz

agent_control:
  path: ./%s
  sha256: "%s"

agent_compute:
  path: ./%s
  sha256: "%s"

config:
  path: ./%s
  sha256: "%s"
`, controlName, ch, computeName, coh, configName, cfh)

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	now := time.Now()

	addBytes := func(name string, body []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), ModTime: now}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}
	addFile := func(name, src string, mode int64) error {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return addBytes(name, data, mode)
	}
	if err := addBytes(bundleManifestName, []byte(manifestBody), 0644); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := addFile(controlName, controlPath, 0755); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := addFile(computeName, computePath, 0755); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := addFile(configName, configPath, 0644); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

// writeBundleTarGzLegacy rebuilds manifest v1 from a single agent + config (older staging trees).
func writeBundleTarGzLegacy(w io.Writer, agentPath, configPath string) error {
	ah, err := fileSHA256Hex(agentPath)
	if err != nil {
		return err
	}
	ch, err := fileSHA256Hex(configPath)
	if err != nil {
		return err
	}
	agentName := filepath.Base(agentPath)
	configName := filepath.Base(configPath)
	manifestBody := fmt.Sprintf(`manifestVersion: 1

bundle:
  format: tar.gz

agent:
  path: ./%s
  sha256: "%s"

config:
  path: ./%s
  sha256: "%s"
`, agentName, ah, configName, ch)

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	now := time.Now()
	add := func(name string, body []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), ModTime: now}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}
	if err := add(bundleManifestName, []byte(manifestBody), 0644); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	agentData, err := os.ReadFile(agentPath)
	if err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: agentName, Mode: 0755, Size: int64(len(agentData)), ModTime: now}); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if _, err := tw.Write(agentData); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	cfgData, err := os.ReadFile(configPath)
	if err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := add(configName, cfgData, 0644); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

func maxBundleUnpackedBytes(maxRequest int64) int64 {
	if maxRequest <= 0 {
		maxRequest = 64 << 20
	}
	u := maxRequest * 5
	const capB = 2 << 30 // 2 GiB max extracted
	if u > capB {
		u = capB
	}
	return u
}

// PrepareAgentBundleFromReader runs the same validation as POST /upload.
func PrepareAgentBundleFromReader(baseDir string, bundleReader io.Reader, maxRequestBytes int64) (*PreparedBundle, error) {
	return prepareAgentBundle(baseDir, bundleReader, maxRequestBytes)
}

func prepareAgentBundle(base string, bundleReader io.Reader, maxRequestBytes int64) (*PreparedBundle, error) {
	workDir := filepath.Join(base, ".bundle-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, err
	}
	bundlePath := filepath.Join(workDir, "upload.tar.gz")
	bf, err := os.Create(bundlePath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	_, err = io.Copy(bf, bundleReader)
	if cerr := bf.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("save bundle: %w", err)
	}

	extractRoot := filepath.Join(workDir, "root")
	if err = os.MkdirAll(extractRoot, 0755); err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	rf, err := os.Open(bundlePath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	err = extractTarGzSafe(rf, extractRoot, maxBundleUnpackedBytes(maxRequestBytes))
	_ = rf.Close()
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("extract bundle: %w", err)
	}

	mf := filepath.Join(extractRoot, bundleManifestName)
	raw, err := os.ReadFile(mf)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("missing manifest file (%s)", bundleManifestName)
	}
	m, err := parseBundleManifest(raw)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}

	configPath, err := bundleMemberAbs(extractRoot, m.Config.Path)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("config.path: %w", err)
	}
	if err := verifyBundleFileHash(configPath, m.Config, "config"); err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	configFileName, err := basenameFromManifestPath(m.Config.Path)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	if _, err := agentcfg.LoadFromBytes(configData); err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}

	refs, err := agentRefsFromManifest(m)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	var agents []PreparedBundleAgent
	for i, ref := range refs {
		label := "agent"
		if m.ManifestVersion == bundleManifestV2 {
			if i == 0 {
				label = "agent_control"
			} else {
				label = "agent_compute"
			}
		}
		agentPath, err := bundleMemberAbs(extractRoot, ref.Path)
		if err != nil {
			_ = os.RemoveAll(workDir)
			return nil, fmt.Errorf("%s.path: %w", label, err)
		}
		if err := verifyBundleFileHash(agentPath, ref, label); err != nil {
			_ = os.RemoveAll(workDir)
			return nil, err
		}
		destName, err := basenameFromManifestPath(ref.Path)
		if err != nil {
			_ = os.RemoveAll(workDir)
			return nil, err
		}
		if err := validateAgentELF(agentPath); err != nil {
			_ = os.RemoveAll(workDir)
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		agents = append(agents, PreparedBundleAgent{DestBasename: destName, ExtractPath: agentPath})
	}

	primary := agents[0].ExtractPath
	versionKey, err := versionKeyFromAgentBinary(primary)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return nil, err
	}
	for i := 1; i < len(agents); i++ {
		vk, err := versionKeyFromAgentBinary(agents[i].ExtractPath)
		if err != nil {
			_ = os.RemoveAll(workDir)
			return nil, fmt.Errorf("agent %s: %w", agents[i].DestBasename, err)
		}
		if vk != versionKey {
			_ = os.RemoveAll(workDir)
			return nil, fmt.Errorf("agent binaries report different version keys (%q vs %q); use matching builds or separate bundles", versionKey, vk)
		}
	}

	return &PreparedBundle{
		VersionKey:     versionKey,
		ConfigData:     configData,
		ConfigFileName: configFileName,
		Agents:         agents,
		BundlePath:     bundlePath,
		WorkDir:        workDir,
	}, nil
}

// StagePreparedBundle copies validated bundle members into staging/<version>/.
func StagePreparedBundle(finalDir string, pb *PreparedBundle) error {
	if pb == nil {
		return errors.New("prepared bundle is nil")
	}
	for _, ag := range pb.Agents {
		srcf, err := os.Open(ag.ExtractPath)
		if err != nil {
			return fmt.Errorf("open %s: %w", ag.DestBasename, err)
		}
		dst := filepath.Join(finalDir, ag.DestBasename)
		dstf, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			_ = srcf.Close()
			return fmt.Errorf("write %s: %w", ag.DestBasename, err)
		}
		_, err = io.Copy(dstf, srcf)
		_ = srcf.Close()
		_ = dstf.Close()
		if err != nil {
			return fmt.Errorf("copy %s: %w", ag.DestBasename, err)
		}
	}
	cfgName := pb.ConfigFileName
	if strings.TrimSpace(cfgName) == "" {
		cfgName = appmeta.ConfigFileName
	}
	if err := os.WriteFile(filepath.Join(finalDir, cfgName), pb.ConfigData, 0644); err != nil {
		return fmt.Errorf("write %s: %w", cfgName, err)
	}
	if versionsapi.StagingHasDualAgents(finalDir) {
		return nil
	}
	// Manifest v1 or single agent: stage canonical name immediately.
	canonicalSrc := filepath.Join(finalDir, pb.Agents[0].DestBasename)
	canonicalDst := filepath.Join(finalDir, appmeta.BinaryName)
	if pb.Agents[0].DestBasename != appmeta.BinaryName {
		if err := copyFile(canonicalSrc, canonicalDst, 0755); err != nil {
			return fmt.Errorf("copy canonical binary %s: %w", appmeta.BinaryName, err)
		}
	}
	return validateAgentBinary(canonicalDst)
}
