package config

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// VersionKey returns the directory / comparison key: "<semver>-<patch>".
// patch is numeric (no fixed width); compare with CompareVersionKeys, not raw strings.
// AgentVersion may use any number of dot-separated numeric segments (e.g. 1.2.3 or 1.2.3.4); there is no fixed "three-part" limit.
func VersionKey(semver string, patch int) string {
	semver = strings.TrimSpace(semver)
	if patch < 0 {
		patch = 0
	}
	return fmt.Sprintf("%s-%d", semver, patch)
}

// SplitVersionKey splits a version key into semver part and numeric patch.
// Canonical format ends with "-<digits>" (new). Legacy on-disk dirs may use "_<digits>" instead; both are accepted.
// The rightmost '_' or '-' that is followed only by ASCII digits defines the patch; semver is the prefix before it.
// Dirs without such a suffix (e.g. "0.4.0" only) yield patch 0.
func SplitVersionKey(s string) (semver string, patch int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0
	}
	for i := len(s) - 2; i >= 0; i-- {
		if s[i] != '_' && s[i] != '-' {
			continue
		}
		suffix := s[i+1:]
		if len(suffix) == 0 {
			continue
		}
		allDig := true
		for j := 0; j < len(suffix); j++ {
			if suffix[j] < '0' || suffix[j] > '9' {
				allDig = false
				break
			}
		}
		if !allDig {
			continue
		}
		p, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		return s[:i], p
	}
	return s, 0
}

// CompareVersionKeys compares two keys: semver (numeric tuple) first, then patch as integer.
// Returns a positive value if a is newer than b, negative if older, 0 if equal.
func CompareVersionKeys(a, b string) int {
	aSem, aPatch := SplitVersionKey(a)
	bSem, bPatch := SplitVersionKey(b)
	cmp := compareSemverStrings(aSem, bSem)
	if cmp != 0 {
		return cmp
	}
	if aPatch > bPatch {
		return 1
	}
	if aPatch < bPatch {
		return -1
	}
	return 0
}

// StagingUpdateAvailable reports whether staged content should allow "apply update" vs current install.
// If semver differs, the previous policy applies: any different directory name allows apply (including downgrade).
// If semver is equal, apply is allowed only when staging patch is greater than current patch.
// Empty current means nothing installed; any non-empty staging is applicable.
func StagingUpdateAvailable(stagingKey, currentKey string) bool {
	if currentKey == "" {
		return stagingKey != ""
	}
	sSem, sPatch := SplitVersionKey(stagingKey)
	cSem, cPatch := SplitVersionKey(currentKey)
	if !semverEqualStrings(sSem, cSem) {
		return stagingKey != currentKey
	}
	return sPatch > cPatch
}

func semverEqualStrings(a, b string) bool {
	va, vb := parseSemverInts(a), parseSemverInts(b)
	if va != nil && vb != nil {
		if len(va) != len(vb) {
			return false
		}
		for i := range va {
			if va[i] != vb[i] {
				return false
			}
		}
		return true
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func compareSemverStrings(a, b string) int {
	va, vb := parseSemverInts(a), parseSemverInts(b)
	if va != nil && vb != nil {
		for k := 0; k < len(va) || k < len(vb); k++ {
			var na, nb int
			if k < len(va) {
				na = va[k]
			}
			if k < len(vb) {
				nb = vb[k]
			}
			if na != nb {
				if na > nb {
					return 1
				}
				return -1
			}
		}
		return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
}

func parseSemverInts(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		i := 0
		for i < len(p) && p[i] >= '0' && p[i] <= '9' {
			i++
		}
		if i == 0 {
			return nil
		}
		n, err := strconv.Atoi(p[:i])
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// ParseVersionKeyFromYAML reads AgentVersion and PatchVersion from YAML and returns the combined key.
func ParseVersionKeyFromYAML(data []byte) (string, error) {
	var f FileConfig
	if err := yaml.Unmarshal(data, &f); err != nil {
		return "", err
	}
	v := strings.TrimSpace(f.Maintenance.AgentVersion)
	if v == "" {
		return "", fmt.Errorf("empty AgentVersion")
	}
	if f.Maintenance.PatchVersion < 0 {
		return "", fmt.Errorf("PatchVersion must be >= 0")
	}
	return VersionKey(v, f.Maintenance.PatchVersion), nil
}

// ValidateSemverField returns an error if s contains a character not allowed in the YAML version field.
func ValidateSemverField(s string) error {
	if s == "" || s == "." || s == ".." {
		return fmt.Errorf("invalid version")
	}
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' {
			continue
		}
		return fmt.Errorf("version field: invalid character")
	}
	return nil
}

// ValidateVersionKeyPath returns an error if the combined key is unsafe for a directory name.
func ValidateVersionKeyPath(s string) error {
	if s == "" || s == "." || s == ".." {
		return fmt.Errorf("invalid version key")
	}
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			continue
		}
		return fmt.Errorf("version key: invalid character")
	}
	return nil
}
