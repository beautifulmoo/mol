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
	// MaxUploadBytes is the max multipart body size for POST /upload and multipart apply-update (agent + config).
	// YAML: integer bytes, or string "64 << 20" / "67108864". Omitted uses DefaultMaxUploadBytes. 0 → server default.
	MaxUploadBytes uploadBytesExpr `yaml:"MaxUploadBytes"`
	// RemoteHealth configures HTTP remote host health checks (maintenance web → remote Server.HTTPPort GET …/health). Browser polls only while the page is open.
	RemoteHealth RemoteHealthConfig `yaml:"RemoteHealth"`
}

// RemoteHealthConfig holds nested Maintenance.RemoteHealth settings.
type RemoteHealthConfig struct {
	IntervalSeconds  int `yaml:"IntervalSeconds"`  // default 10; base seconds between checks (plus jitter)
	TimeoutSeconds   int `yaml:"TimeoutSeconds"`   // default 2; per-request HTTP timeout
	FailureThreshold int `yaml:"FailureThreshold"` // default 3; consecutive failures → UI "dead"
	JitterSeconds    int `yaml:"JitterSeconds"`    // default 2; random [0,jitter] seconds added each interval
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

// DefaultMaxUploadBytes is the default max POST body size for /upload and multipart apply-update (same as the former maxUploadBytes constant).
const DefaultMaxUploadBytes = 64 << 20

// Default returns default configuration values.
func Default() Config {
	c := Config{
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
		MaxUploadBytes: uploadBytesExpr(DefaultMaxUploadBytes),
		RemoteHealth: RemoteHealthConfig{
			IntervalSeconds:  10,
			TimeoutSeconds:   2,
			FailureThreshold: 3,
			JitterSeconds:    2,
		},
	}
	normalizeRemoteHealthCheck(&c)
	return c
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
	normalizeRemoteHealthCheck(&f.Maintenance)
	return &f.Maintenance, nil
}

// normalizeRemoteHealthCheck applies defaults and sane bounds after YAML load.
func normalizeRemoteHealthCheck(c *Config) {
	rh := &c.RemoteHealth
	if rh.IntervalSeconds <= 0 {
		rh.IntervalSeconds = 10
	}
	if rh.IntervalSeconds > 86400 {
		rh.IntervalSeconds = 86400
	}
	if rh.TimeoutSeconds <= 0 {
		rh.TimeoutSeconds = 2
	}
	if rh.TimeoutSeconds > 120 {
		rh.TimeoutSeconds = 120
	}
	if rh.FailureThreshold <= 0 {
		rh.FailureThreshold = 3
	}
	if rh.FailureThreshold > 100 {
		rh.FailureThreshold = 100
	}
	if rh.JitterSeconds < 0 {
		rh.JitterSeconds = 0
	}
	if rh.JitterSeconds > 300 {
		rh.JitterSeconds = 300
	}
}

// configValidationError turns a YAML unmarshal error into a user-friendly message.
func configValidationError(err error) error {
	if err == nil {
		return nil
	}
	prefix := "config validation failed: "
	// *yaml.TypeError contains multiple errors (e.g. "line 5: cannot unmarshal !!str into int")
	if yerr, ok := err.(*yaml.TypeError); ok && len(yerr.Errors) > 0 {
		msgs := make([]string, 0, len(yerr.Errors))
		for _, e := range yerr.Errors {
			msgs = append(msgs, describeYAMLUnmarshalError(e))
		}
		return fmt.Errorf("%s%s. Expected types include: Server.HTTPPort (int), DiscoveryServiceName (string), DiscoveryUDPPort (int), MaintenancePort (int), DiscoveryTimeoutSeconds (int)", prefix, strings.Join(msgs, "; "))
	}
	// Syntax error (e.g. invalid indentation)
	if strings.Contains(err.Error(), "yaml:") {
		return fmt.Errorf("%s%v. Check YAML syntax (indentation, colons, etc.)", prefix, err)
	}
	return fmt.Errorf("%s%w", prefix, err)
}

// describeYAMLUnmarshalError maps a single unmarshal error to a short description.
func describeYAMLUnmarshalError(s string) string {
	// Typical: "line 5: cannot unmarshal !!str `hello` into int"
	if strings.Contains(s, "cannot unmarshal !!str") && strings.Contains(s, "into int") {
		if line := extractLine(s); line != "" {
			return line + "string value where a number is required (DiscoveryUDPPort, MaintenancePort, DiscoveryTimeoutSeconds must be integers)"
		}
		return "string value where a number is required (DiscoveryUDPPort, MaintenancePort, DiscoveryTimeoutSeconds must be integers)"
	}
	if strings.Contains(s, "cannot unmarshal !!int") && strings.Contains(s, "into string") {
		if line := extractLine(s); line != "" {
			return line + "number where a string is required"
		}
		return "number where a string is required"
	}
	if strings.Contains(s, "cannot unmarshal !!bool") {
		if line := extractLine(s); line != "" {
			return line + "wrong type (expected non-boolean)"
		}
		return "wrong type (expected non-boolean)"
	}
	if line := extractLine(s); line != "" {
		// Avoid duplicating "line N:" in message
		if lineRegex.MatchString(s) {
			return line + "format or type error (check field names and types on that line)"
		}
		return line + s
	}
	return s
}

var lineRegex = regexp.MustCompile(`line (\d+)`)

func extractLine(s string) string {
	if m := lineRegex.FindStringSubmatch(s); len(m) >= 2 {
		return "line " + m[1] + ": "
	}
	return ""
}
