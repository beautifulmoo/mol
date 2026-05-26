package appmeta

import (
	"fmt"
	"strings"
)

// AgentVariantControl / AgentVariantCompute select which bundle binary becomes BinaryName at apply time.
const (
	AgentVariantControl = "control"
	AgentVariantCompute = "compute"
)

// ParseAgentVariant normalizes agent_variant from API/CLI. Empty string defaults to compute.
func ParseAgentVariant(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return AgentVariantCompute, nil
	}
	switch v {
	case AgentVariantControl, AgentVariantCompute:
		return v, nil
	default:
		return "", fmt.Errorf("agent_variant must be %q or %q", AgentVariantControl, AgentVariantCompute)
	}
}

// BundleAgentBasenameForVariant returns the staged tar member basename for the variant.
func BundleAgentBasenameForVariant(variant string) string {
	if variant == AgentVariantCompute {
		return BundleAgentComputeName
	}
	return BundleAgentControlName
}
