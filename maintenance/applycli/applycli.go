// Package applycli implements `contrabass-moleU --apply-update` (bundle preflight + upload + apply in one shot).
package applycli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/server"
)

const bundleFormField = "bundle" // same as server.uploadBundleField

// Run parses flags and runs apply-update CLI. Expected invocation:
//
//	<bin> --apply-update -cfg <config.yaml> <self|remote-ip> <bundle.tar.gz>
func Run(args []string) int {
	fs := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("cfg", "", "설정 파일 경로 (필수)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --apply-update -cfg <config.yaml> <self|원격IP> <bundle.tar.gz>\n\n", appmeta.BinaryName)
		fmt.Fprintf(os.Stderr, "  번들을 검증한 뒤 현재 버전과 비교하여 업데이트 가능할 때만 업로드·적용합니다.\n")
		fmt.Fprintf(os.Stderr, "  self: 로컬 유지보수 API로 업로드 후 적용합니다.\n")
		fmt.Fprintf(os.Stderr, "  원격IP: 로컬 유지보수 API의 multipart apply-update로 원격 호스트에 직접 전송합니다.\n\n")
		fs.PrintDefaults()
	}
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fs.Usage()
			return 0
		}
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	pos := fs.Args()
	if len(pos) != 2 {
		fmt.Fprintf(os.Stderr, "%s: 인자는 <self|원격IP> <bundle.tar.gz> 두 개여야 합니다.\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}
	if strings.TrimSpace(*cfgPath) == "" {
		fmt.Fprintf(os.Stderr, "%s: -cfg <설정 파일> 이 필요합니다.\n", appmeta.BinaryName)
		fs.Usage()
		return 1
	}

	target := strings.TrimSpace(pos[0])
	bundlePath := strings.TrimSpace(pos[1])
	if target == "" || bundlePath == "" {
		fmt.Fprintf(os.Stderr, "%s: 대상과 번들 경로가 비어 있으면 안 됩니다.\n", appmeta.BinaryName)
		return 1
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: 설정 로드: %v\n", appmeta.BinaryName, err)
		return 1
	}

	maxBytes := normalizeMaxUploadBytes(cfg.MaxUploadBytes.Int())

	f, err := os.Open(bundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: 번들 열기: %v\n", appmeta.BinaryName, err)
		return 1
	}
	bundleReader := io.Reader(f)
	if fi, err := f.Stat(); err == nil && fi.Size() > maxBytes {
		_ = f.Close()
		fmt.Fprintf(os.Stderr, "%s: 번들 크기(%d)가 설정 한도(%d)를 초과합니다.\n", appmeta.BinaryName, fi.Size(), maxBytes)
		return 1
	}

	versionKey, _, _, workDir, _, err := server.PrepareAgentBundleFromReader(os.TempDir(), bundleReader, maxBytes)
	_ = f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: 번들 검증 실패: %v\n", appmeta.BinaryName, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	maintenanceBase := maintenanceHTTPBase(cfg)
	apiPrefix := normalizeAPIPrefix(cfg.APIPrefix)

	httpClient := &http.Client{Timeout: 300 * time.Second}

	switch strings.ToLower(target) {
	case "self":
		cur, err := fetchVersionGET(httpClient, maintenanceBase+apiPrefix+"/self")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: 로컬 버전 조회 실패 (%s): %v\n", appmeta.BinaryName, maintenanceBase+apiPrefix+"/self", err)
			return 1
		}
		if !config.StagingUpdateAvailable(versionKey, cur) {
			fmt.Fprintf(os.Stderr, "%s: 업데이트 불필요 또는 정책상 건너뜀 (번들 %q, 현재 %q).\n", appmeta.BinaryName, versionKey, cur)
			return 1
		}
		fmt.Printf("번들 버전 %s → 로컬 적용 (현재 %s)\n", versionKey, cur)
		if err := postUploadBundle(httpClient, maintenanceBase+apiPrefix+"/upload", bundlePath); err != nil {
			fmt.Fprintf(os.Stderr, "%s: 업로드 실패: %v\n", appmeta.BinaryName, err)
			return 1
		}
		if err := postApplyUpdateJSON(httpClient, maintenanceBase+apiPrefix+"/apply-update", versionKey); err != nil {
			fmt.Fprintf(os.Stderr, "%s: 적용 요청 실패: %v\n", appmeta.BinaryName, err)
			return 1
		}
		fmt.Println("업데이트 적용 요청을 보냈습니다. 잠시 후 에이전트가 재시작됩니다.")
		return 0

	default:
		remoteIP := target
		if net.ParseIP(remoteIP) == nil {
			fmt.Fprintf(os.Stderr, "%s: 원격 대상은 유효한 IP 주소여야 합니다: %q\n", appmeta.BinaryName, remoteIP)
			return 1
		}
		httpPort := cfg.ServerHTTPPort
		if httpPort <= 0 {
			httpPort = 8888
		}
		addr := net.JoinHostPort(remoteIP, strconv.Itoa(httpPort))
		if err := dialTCP(addr, 5*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "%s: 원격 %s 에 연결할 수 없습니다 (에이전트 HTTP 포트가 열려 있어야 합니다): %v\n", appmeta.BinaryName, addr, err)
			return 1
		}
		remoteBase := fmt.Sprintf("http://%s", addr)
		cur, err := fetchVersionGET(httpClient, remoteBase+apiPrefix+"/self")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: 원격 버전 조회 실패 (%s): %v\n", appmeta.BinaryName, remoteBase+apiPrefix+"/self", err)
			return 1
		}
		if !config.StagingUpdateAvailable(versionKey, cur) {
			fmt.Fprintf(os.Stderr, "%s: 업데이트 불필요 또는 정책상 건너뜀 (번들 %q, 원격 현재 %q).\n", appmeta.BinaryName, versionKey, cur)
			return 1
		}
		fmt.Printf("번들 버전 %s → 원격 %s 적용 (원격 현재 %s)\n", versionKey, remoteIP, cur)
		applyURL := maintenanceBase + apiPrefix + "/apply-update"
		if err := postMultipartApplyRemote(httpClient, applyURL, remoteIP, bundlePath); err != nil {
			fmt.Fprintf(os.Stderr, "%s: 원격 적용 실패: %v\n", appmeta.BinaryName, err)
			return 1
		}
		fmt.Printf("원격 %s 에 버전 %s 적용이 완료되었습니다.\n", remoteIP, versionKey)
		return 0
	}
}

func normalizeMaxUploadBytes(n int) int64 {
	const minB = int64(1 << 20)
	const maxB = int64(10 << 30)
	if n <= 0 {
		return int64(config.DefaultMaxUploadBytes)
	}
	v := int64(n)
	if v < minB {
		return minB
	}
	if v > maxB {
		return maxB
	}
	return v
}

func maintenanceHTTPBase(cfg *config.Config) string {
	host := strings.TrimSpace(cfg.MaintenanceListenAddress)
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := cfg.MaintenancePort
	if port <= 0 {
		port = 8889
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func normalizeAPIPrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/api/v1"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}

func dialTCP(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func fetchVersionGET(client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("JSON: %w", err)
	}
	if out.Status != "success" {
		return "", fmt.Errorf("status %q", out.Status)
	}
	return strings.TrimSpace(out.Data.Version), nil
}

func postUploadBundle(client *http.Client, uploadURL, bundlePath string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(bundleFormField, filepath.Base(bundlePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, uploadURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out server.APIResponse
	if json.Unmarshal(body, &out) != nil {
		return fmt.Errorf("업로드 응답 파싱 실패: %s", strings.TrimSpace(string(body)))
	}
	if out.Status != "success" {
		if s, ok := out.Data.(string); ok && s != "" {
			return fmt.Errorf("%s", s)
		}
		return fmt.Errorf("업로드 실패: status=%s", out.Status)
	}
	return nil
}

func postApplyUpdateJSON(client *http.Client, applyURL, version string) error {
	payload, err := json.Marshal(map[string]string{"version": version})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, applyURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out server.APIResponse
	if json.Unmarshal(body, &out) != nil {
		return fmt.Errorf("적용 응답 파싱 실패: %s", strings.TrimSpace(string(body)))
	}
	if out.Status != "success" {
		if s, ok := out.Data.(string); ok && s != "" {
			return fmt.Errorf("%s", s)
		}
		return fmt.Errorf("적용 실패: status=%s", out.Status)
	}
	return nil
}

func postMultipartApplyRemote(client *http.Client, applyURL, remoteIP, bundlePath string) error {
	f, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("ip", remoteIP); err != nil {
		return err
	}
	part, err := w.CreateFormFile(bundleFormField, filepath.Base(bundlePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, applyURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out server.APIResponse
	if json.Unmarshal(body, &out) != nil {
		return fmt.Errorf("원격 적용 응답 파싱 실패: %s", strings.TrimSpace(string(body)))
	}
	if out.Status != "success" {
		if s, ok := out.Data.(string); ok && s != "" {
			return fmt.Errorf("%s", s)
		}
		return fmt.Errorf("원격 적용 실패: status=%s", out.Status)
	}
	return nil
}
