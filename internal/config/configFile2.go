package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds application configuration (YAML).
type Config struct {
	DiscoveryServiceName       string `yaml:"DiscoveryServiceName"`
	DiscoveryBroadcastAddress  string `yaml:"DiscoveryBroadcastAddress"` // fallback when automatic brd collection (PRD 3.1.1) finds none
	// DiscoveryBroadcastAddresses []string `yaml:"DiscoveryBroadcastAddresses"` // 주석: 물리 NIC brd 자동 수집 사용
	DiscoveryUDPPort           int    `yaml:"DiscoveryUDPPort"`
	MaintenanceListenAddress   string `yaml:"MaintenanceListenAddress"` // e.g. "127.0.0.1" (internal only) or "0.0.0.0"
	MaintenancePort            int    `yaml:"MaintenancePort"`
	ServerHTTPPort             int    `yaml:"-"` // from top-level Server.HTTPPort (Gin). Used for remote calls.
	WebPrefix                  string `yaml:"WebPrefix"`
	APIPrefix                  string `yaml:"APIPrefix"`
	DiscoveryTimeoutSeconds    int    `yaml:"DiscoveryTimeoutSeconds"`
	DiscoveryDeduplicate bool `yaml:"DiscoveryDeduplicate"`
	// Systemctl service status (self + discovered hosts)
	SystemctlServiceName string `yaml:"SystemctlServiceName"` // e.g. "contrabass-mole.service"
	DeployBase           string `yaml:"DeployBase"`           // e.g. "/var/lib/contrabass/mole" for staging/, update.sh
	InstallPrefix        string `yaml:"InstallPrefix"`        // contrabass-moleU 설치 경로 prefix (versions/ 목록·삭제, installer 등). 비면 deploy_base 사용
	// SSH for remote service start/stop (when remote contrabass-moleU is stopped, API is unreachable)
	SSHPort int    `yaml:"SSHPort"` // default 22; used for ssh -p when starting/stopping remote contrabass-moleU (systemctl) on the remote host
	SSHUser string `yaml:"SSHUser"` // default "root"; user for ssh to remote host
}

// FileConfig is the on-disk YAML shape:
//
//	Maintenance:
//	  MaintenancePort: 8889
//	  ...
type FileConfig struct {
	Server      ServerConfig `yaml:"Server"`
	Maintenance Config       `yaml:"Maintenance"`
}

type ServerConfig struct {
	HTTPPort int `yaml:"HTTPPort"`
}

// DefaultDiscoveryServiceName is the default DISCOVERY_REQUEST `service` value (must match Maintenance.DiscoveryServiceName).
const DefaultDiscoveryServiceName = "Mole-Discovery"

// Default returns default configuration values.
func Default() Config {
	return Config{
		DiscoveryServiceName:      DefaultDiscoveryServiceName,
		DiscoveryBroadcastAddress: "192.168.0.255",
		DiscoveryUDPPort:          9999,
		MaintenanceListenAddress:  "127.0.0.1",
		MaintenancePort:           0,
		ServerHTTPPort:            0,
		WebPrefix:                 "/web",
		APIPrefix:                 "/api/v1",
		DiscoveryTimeoutSeconds:   10,
		DiscoveryDeduplicate:      true,
		SystemctlServiceName:      "contrabass-mole.service",
		DeployBase:                "/var/lib/contrabass/mole",
		SSHPort:                   22,
		SSHUser:                   "root",
	}
}

// Load reads config from path. If path is empty, "config.yaml" in the current directory is used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = "config.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return LoadFromBytes(data)
}

// LoadFromBytes parses config from YAML bytes. Used for upload validation.
// Returns a descriptive error (which field/line caused the error, what type is expected) when parsing fails.
func LoadFromBytes(data []byte) (*Config, error) {
	f := FileConfig{Maintenance: Default()}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, configValidationError(err)
	}
	f.Maintenance.ServerHTTPPort = f.Server.HTTPPort
	return &f.Maintenance, nil
}

// configValidationError turns a YAML unmarshal error into a user-friendly message in Korean.
func configValidationError(err error) error {
	if err == nil {
		return nil
	}
	prefix := "config.yaml 검증 실패: "
	// *yaml.TypeError contains multiple errors (e.g. "line 5: cannot unmarshal !!str into int")
	if yerr, ok := err.(*yaml.TypeError); ok && len(yerr.Errors) > 0 {
		msgs := make([]string, 0, len(yerr.Errors))
		for _, e := range yerr.Errors {
			msgs = append(msgs, describeYAMLUnmarshalError(e))
		}
		return fmt.Errorf("%s%s. 필요한 항목 및 타입: Server.HTTPPort(숫자), DiscoveryServiceName(문자열), DiscoveryUDPPort(숫자), MaintenancePort(숫자), DiscoveryTimeoutSeconds(숫자) 등", prefix, strings.Join(msgs, "; "))
	}
	// Syntax error (e.g. invalid indentation)
	if strings.Contains(err.Error(), "yaml:") {
		return fmt.Errorf("%s%v. YAML 형식(들여쓰기, 콜론 뒤 공백 등)을 확인하세요", prefix, err)
	}
	return fmt.Errorf("%s%w", prefix, err)
}

// describeYAMLUnmarshalError maps a single unmarshal error to a short Korean description.
func describeYAMLUnmarshalError(s string) string {
	// Typical: "line 5: cannot unmarshal !!str `hello` into int"
	if strings.Contains(s, "cannot unmarshal !!str") && strings.Contains(s, "into int") {
		if line := extractLine(s); line != "" {
			return line + " 숫자 항목에 문자열이 들어갔습니다 (DiscoveryUDPPort, MaintenancePort, DiscoveryTimeoutSeconds 등은 숫자여야 함)"
		}
		return "숫자 항목에 문자열이 들어갔습니다 (DiscoveryUDPPort, MaintenancePort, DiscoveryTimeoutSeconds 등은 숫자여야 함)"
	}
	if strings.Contains(s, "cannot unmarshal !!int") && strings.Contains(s, "into string") {
		if line := extractLine(s); line != "" {
			return line + " 문자열 항목에 숫자가 들어갔습니다"
		}
		return "문자열 항목에 숫자가 들어갔습니다"
	}
	if strings.Contains(s, "cannot unmarshal !!bool") {
		if line := extractLine(s); line != "" {
			return line + " 항목 타입이 맞지 않습니다 (불리언이 아닌 값 필요)"
		}
		return "항목 타입이 맞지 않습니다"
	}
	if line := extractLine(s); line != "" {
		// Avoid duplicating "line N:" in message
		if lineRegex.MatchString(s) {
			return line + "형식 또는 타입 오류 (해당 줄의 항목명·타입 확인)"
		}
		return line + s
	}
	return s
}

var lineRegex = regexp.MustCompile(`line (\d+)`)

func extractLine(s string) string {
	if m := lineRegex.FindStringSubmatch(s); len(m) >= 2 {
		return m[1] + "번째 줄: "
	}
	return ""
}
