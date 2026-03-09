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
	ServiceName                 string   `yaml:"service_name"`
	DiscoveryBroadcastAddress   string   `yaml:"discovery_broadcast_address"`   // single; used if discovery_broadcast_addresses is empty
	DiscoveryBroadcastAddresses []string `yaml:"discovery_broadcast_addresses"`  // optional list; overrides single when non-empty
	DiscoveryUDPPort            int      `yaml:"discovery_udp_port"`
	HTTPPort                   int    `yaml:"http_port"`
	WebPrefix                  string `yaml:"web_prefix"`
	APIPrefix                  string `yaml:"api_prefix"`
	DiscoveryTimeoutSeconds    int    `yaml:"discovery_timeout_seconds"`
	DiscoveryDeduplicate       bool   `yaml:"discovery_deduplicate"`
	Version                    string `yaml:"version"`
	// Systemctl service status (self + discovered hosts)
	SystemctlServiceName string `yaml:"systemctl_service_name"` // e.g. "mol.service"
	DeployBase           string `yaml:"deploy_base"`             // e.g. "/opt/mol" for versions/ and update.sh
	// SSH for remote service start/stop (when remote mol is stopped, API is unreachable)
	SSHPort int    `yaml:"ssh_port"` // default 22; used for ssh -p when starting/stopping remote mol service
	SSHUser string `yaml:"ssh_user"` // default "root"; user for ssh to remote host
}

// Default returns default configuration values.
func Default() Config {
	return Config{
		ServiceName:               "mol",
		DiscoveryBroadcastAddress: "192.168.0.255",
		DiscoveryUDPPort:          9999,
		HTTPPort:                  8888,
		WebPrefix:                 "/web",
		APIPrefix:                 "/api/v1",
		DiscoveryTimeoutSeconds:   10,
		DiscoveryDeduplicate:      true,
		Version:                   "",
		SystemctlServiceName:      "mol.service",
		DeployBase:                "/opt/mol",
		SSHPort:                   22,
		SSHUser:                   "root",
	}
}

// ParseVersionFromYAML extracts the "version" field from YAML bytes (e.g. uploaded config.yaml).
func ParseVersionFromYAML(data []byte) (string, error) {
	var v struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &v); err != nil {
		return "", err
	}
	return strings.TrimSpace(v.Version), nil
}

// Load reads config from path. If path is empty, env MOL_CONFIG is used; else "config.yaml".
func Load(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv("MOL_CONFIG")
	}
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
	c := Default()
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, configValidationError(err)
	}
	return &c, nil
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
		return fmt.Errorf("%s%s. 필요한 항목 및 타입: service_name(문자열), discovery_udp_port(숫자), http_port(숫자), discovery_timeout_seconds(숫자), version(문자열) 등", prefix, strings.Join(msgs, "; "))
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
			return line + " 숫자 항목에 문자열이 들어갔습니다 (discovery_udp_port, http_port, discovery_timeout_seconds 등은 숫자여야 함)"
		}
		return "숫자 항목에 문자열이 들어갔습니다 (discovery_udp_port, http_port, discovery_timeout_seconds 등은 숫자여야 함)"
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
