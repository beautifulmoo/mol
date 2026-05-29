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

// ParseAgentVariant normalizes an explicit agent_variant from API/CLI. Empty string defaults to compute.
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

// DefaultAgentVariantFromBuild returns control|compute from installed build_variant (e.g. GET /self), else compute.
func DefaultAgentVariantFromBuild(buildVariant string) string {
	v := strings.ToLower(strings.TrimSpace(buildVariant))
	if v == AgentVariantControl || v == AgentVariantCompute {
		return v
	}
	return AgentVariantCompute
}

// ResolveAgentVariantForApply uses explicit when set; otherwise installed build_variant on the apply target.
func ResolveAgentVariantForApply(explicit, installedBuildVariant string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return ParseAgentVariant(explicit)
	}
	return DefaultAgentVariantFromBuild(installedBuildVariant), nil
}

// ParseBuildVariantFromVersionLine extracts control|compute from a --version line suffix, e.g. "… (compute)".
func ParseBuildVariantFromVersionLine(line string) string {
	line = strings.TrimSpace(line)
	want := BinaryName + " "
	if !strings.HasPrefix(line, want) {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, want))
	idx := strings.Index(rest, " (")
	if idx < 0 || !strings.HasSuffix(rest, ")") {
		return ""
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(rest[idx:], " ("), ")")
	v := strings.ToLower(strings.TrimSpace(inner))
	if v == AgentVariantControl || v == AgentVariantCompute {
		return v
	}
	return ""
}

// BundleAgentBasenameForVariant returns the staged tar member basename for the variant.
func BundleAgentBasenameForVariant(variant string) string {
	if variant == AgentVariantCompute {
		return BundleAgentComputeName
	}
	return BundleAgentControlName
}
