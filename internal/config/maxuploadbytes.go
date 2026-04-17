package config

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// uploadBytesExpr is Maintenance.MaxUploadBytes: YAML int, or string "64 << 20" / "67108864".
type uploadBytesExpr int

// Int returns the value for server clamping (0 means “use default” there).
func (u uploadBytesExpr) Int() int { return int(u) }

var maxUploadShiftExpr = regexp.MustCompile(`^\s*(\d+)\s*<<\s*(\d+)\s*$`)

// parseMaxUploadBytesExpr accepts a decimal string or "M << N" (unsigned shift, same as Go).
func parseMaxUploadBytesExpr(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	if strings.Contains(s, "<<") {
		m := maxUploadShiftExpr.FindStringSubmatch(s)
		if m == nil {
			return 0, fmt.Errorf("%q is not supported (use \"M << N\", e.g. \"64 << 20\")", s)
		}
		a, err1 := strconv.ParseUint(m[1], 10, 64)
		b, err2 := strconv.ParseUint(m[2], 10, 64)
		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("invalid bit-shift operands")
		}
		if b > 62 {
			return 0, fmt.Errorf("shift count %d too large (max 62)", b)
		}
		res := a << b
		if res > uint64(math.MaxInt) {
			return 0, fmt.Errorf("result too large")
		}
		return int(res), nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("expected integer or \"M << N\": %w", err)
	}
	if n < 0 {
		return 0, fmt.Errorf("cannot be negative")
	}
	return n, nil
}

func (u *uploadBytesExpr) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "null" || n.Value == "~") {
		*u = 0
		return nil
	}
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("Maintenance.MaxUploadBytes: must be a scalar (number or string)")
	}
	var i int64
	if err := n.Decode(&i); err == nil {
		if i < 0 {
			return fmt.Errorf("Maintenance.MaxUploadBytes: cannot be negative")
		}
		*u = uploadBytesExpr(i)
		return nil
	}
	var f float64
	if err := n.Decode(&f); err == nil {
		if f < 0 {
			return fmt.Errorf("Maintenance.MaxUploadBytes: cannot be negative")
		}
		if f != math.Trunc(f) {
			return fmt.Errorf("Maintenance.MaxUploadBytes: must be an integer")
		}
		if f > float64(math.MaxInt) {
			return fmt.Errorf("Maintenance.MaxUploadBytes: value too large")
		}
		*u = uploadBytesExpr(f)
		return nil
	}
	var s string
	if err := n.Decode(&s); err != nil {
		return fmt.Errorf("Maintenance.MaxUploadBytes: %w", err)
	}
	v, err := parseMaxUploadBytesExpr(s)
	if err != nil {
		return fmt.Errorf("Maintenance.MaxUploadBytes: %w", err)
	}
	*u = uploadBytesExpr(v)
	return nil
}
