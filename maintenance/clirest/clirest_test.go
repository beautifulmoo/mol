package clirest

import "testing"

func TestIsLocalTarget(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"self", true},
		{"SELF", true},
		{"local", true},
		{"Local", true},
		{"127.0.0.1", false},
		{"172.29.1.1", false},
		{"", false},
		{"localhost", false},
	} {
		if got := IsLocalTarget(tc.in); got != tc.want {
			t.Errorf("IsLocalTarget(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidateTarget_local(t *testing.T) {
	if err := ValidateTarget("local"); err != nil {
		t.Fatalf("ValidateTarget(local): %v", err)
	}
}

func TestResolveHost_local(t *testing.T) {
	if got := resolveHost("local"); got != "127.0.0.1" {
		t.Fatalf("resolveHost(local) = %q", got)
	}
}
