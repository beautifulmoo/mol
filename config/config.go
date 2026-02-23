package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds application configuration (YAML).
type Config struct {
	ServiceName                string `yaml:"service_name"`
	DiscoveryBroadcastAddress  string `yaml:"discovery_broadcast_address"`
	DiscoveryUDPPort           int    `yaml:"discovery_udp_port"`
	HTTPPort                   int    `yaml:"http_port"`
	WebPrefix                  string `yaml:"web_prefix"`
	APIPrefix                  string `yaml:"api_prefix"`
	DiscoveryTimeoutSeconds    int    `yaml:"discovery_timeout_seconds"`
	DiscoveryDeduplicate       bool   `yaml:"discovery_deduplicate"`
	Version                    string `yaml:"version"`
	// Systemctl service status (self + discovered hosts)
	SystemctlServiceName string `yaml:"systemctl_service_name"` // e.g. "mol.service"
	SSHUser              string `yaml:"ssh_user"`               // e.g. "kt" for ssh kt@<ip>
	SSHIdentityFile      string `yaml:"ssh_identity_file"`      // optional: path to private key (required if service runs as non-kt user)
	DeployBase           string `yaml:"deploy_base"`             // e.g. "/opt/mol" for versions/ and update.sh
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
		SystemctlServiceName:     "mol.service",
		SSHUser:                   "kt",
		DeployBase:                "/opt/mol",
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
	c := Default()
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &c, nil
}
