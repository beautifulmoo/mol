package server

import (
	"archive/tar"
	"compress/gzip"
	"contrabass-agent/maintenance/config"
	"contrabass-agent/maintenance/appmeta"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	bundleManifestName = "contrabass.manifest.yaml"
	uploadBundleField  = "bundle"
)

// StagedBundleFileName is the original upload tar.gz kept next to the extracted agent and config
// under staging/<version>/ and versions/<version>/ so remote POST /upload can re-send the same bytes
// without rebuilding the archive (manifest may list arbitrary future files).
const StagedBundleFileName = "upload.bundle.tar.gz"

// maxBundleMembers limits entries processed from a tar.gz (defense in depth).
const maxBundleMembers = 512

// bundleManifestDoc matches maintenance/packaging/contrabass.manifest.yaml (manifestVersion 1).
type bundleManifestDoc struct {
	ManifestVersion int `yaml:"manifestVersion"`
	Agent           struct {
		Path   string `yaml:"path"`
		Sha256 string `yaml:"sha256"`
	} `yaml:"agent"`
	Config struct {
		Path   string `yaml:"path"`
		Sha256 string `yaml:"sha256"`
	} `yaml:"config"`
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
			// GNU tar 등이 `tar ... .` 로 묶을 때 `./` 디렉터리 항목을 넣는다. normalizeBundlePath("./") 는 ""가 되어
			// bundleMemberAbs와 충돌하므로 아카이브 루트 디렉터리는 건너뛴다.
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
	if m.ManifestVersion != 1 {
		return nil, fmt.Errorf("manifestVersion %d not supported (only 1)", m.ManifestVersion)
	}
	if strings.TrimSpace(m.Agent.Path) == "" {
		return nil, errors.New("manifest missing agent.path")
	}
	if strings.TrimSpace(m.Config.Path) == "" {
		return nil, errors.New("manifest missing config.path")
	}
	return &m, nil
}

func verifyBundleMemberHashes(agentPath, configPath string, m *bundleManifestDoc) error {
	ah, err := fileSHA256Hex(agentPath)
	if err != nil {
		return fmt.Errorf("agent hash: %w", err)
	}
	if !sha256Matches(m.Agent.Sha256, ah) {
		return fmt.Errorf("agent sha256 mismatch (manifest vs file)")
	}
	ch, err := fileSHA256Hex(configPath)
	if err != nil {
		return fmt.Errorf("config hash: %w", err)
	}
	if !sha256Matches(m.Config.Sha256, ch) {
		return fmt.Errorf("config sha256 mismatch (manifest vs file)")
	}
	return nil
}

// writeBundleTarGz writes a tar.gz to w containing manifest, agent, config with the canonical layout expected by upload.
func writeBundleTarGz(w io.Writer, agentPath, configPath string) error {
	ah, err := fileSHA256Hex(agentPath)
	if err != nil {
		return err
	}
	ch, err := fileSHA256Hex(configPath)
	if err != nil {
		return err
	}
	manifestBody := fmt.Sprintf(`manifestVersion: 1

bundle:
  format: tar.gz

agent:
  path: ./%s
  sha256: "%s"

config:
  path: ./config.yaml
  sha256: "%s"
`, appmeta.BinaryName, ah, ch)

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
	if err := tw.WriteHeader(&tar.Header{Name: appmeta.BinaryName, Mode: 0755, Size: int64(len(agentData)), ModTime: now}); err != nil {
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
	if err := add("config.yaml", cfgData, 0644); err != nil {
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

// PrepareAgentBundleFromReader runs the same validation as POST /upload: extract tar.gz, manifest, hashes, config YAML, ELF, and version from the agent binary.
// baseDir is only used as the parent for a temporary work directory (e.g. os.TempDir()).
// Caller must os.RemoveAll(workDir) when done.
func PrepareAgentBundleFromReader(baseDir string, bundleReader io.Reader, maxRequestBytes int64) (versionKey string, configData []byte, bundlePath string, workDir string, agentExtractPath string, err error) {
	return prepareAgentBundle(baseDir, bundleReader, maxRequestBytes)
}

// prepareAgentBundle reads a tar.gz stream into base/.bundle-*/, extracts it, validates manifest, hashes, config YAML, ELF, and --version.
// agentExtractPath is the absolute path to the agent binary inside the extracted tree (for copying to staging).
// Caller must os.RemoveAll(workDir) when done (after remote POST if bundlePath is needed).
func prepareAgentBundle(base string, bundleReader io.Reader, maxRequestBytes int64) (versionKey string, configData []byte, bundlePath string, workDir string, agentExtractPath string, err error) {
	workDir = filepath.Join(base, ".bundle-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err = os.MkdirAll(workDir, 0755); err != nil {
		return "", nil, "", "", "", err
	}
	bundlePath = filepath.Join(workDir, "upload.tar.gz")
	bf, err := os.Create(bundlePath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	_, err = io.Copy(bf, bundleReader)
	if cerr := bf.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("save bundle: %w", err)
	}

	extractRoot := filepath.Join(workDir, "root")
	if err = os.MkdirAll(extractRoot, 0755); err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	rf, err := os.Open(bundlePath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	err = extractTarGzSafe(rf, extractRoot, maxBundleUnpackedBytes(maxRequestBytes))
	_ = rf.Close()
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("extract bundle: %w", err)
	}

	mf := filepath.Join(extractRoot, bundleManifestName)
	raw, err := os.ReadFile(mf)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("missing manifest file (%s)", bundleManifestName)
	}
	m, err := parseBundleManifest(raw)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	agentPath, err := bundleMemberAbs(extractRoot, m.Agent.Path)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("agent.path: %w", err)
	}
	configPath, err := bundleMemberAbs(extractRoot, m.Config.Path)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("config.path: %w", err)
	}
	if _, err := os.Stat(agentPath); err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("agent file missing: %s", m.Agent.Path)
	}
	if _, err := os.Stat(configPath); err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("config file missing: %s", m.Config.Path)
	}
	if err := verifyBundleMemberHashes(agentPath, configPath, m); err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	configData, err = os.ReadFile(configPath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	if _, err := config.LoadFromBytes(configData); err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	hdr := make([]byte, 4)
	af, err := os.Open(agentPath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	_, err = io.ReadFull(af, hdr)
	_ = af.Close()
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("executable too short")
	}
	if !isELFExecutable(hdr) {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", fmt.Errorf("not a valid ELF executable")
	}
	versionKey, err = versionKeyFromAgentBinary(agentPath)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", nil, "", "", "", err
	}
	return versionKey, configData, bundlePath, workDir, agentPath, nil
}
