package hostinfo

import "testing"

func TestUselessHostID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"0", true},
		{"00000000-0000-0000-0000-000000000000", true},
		{"Not Settable", true},
		{"To be filled by O.E.M.", true},
		{"03000200-0400-0500-0006-000700080009", false},
		{"a1b2c3d4-e5f6-7890-abcd-ef1234567890", false},
	}
	for _, c := range cases {
		if got := uselessHostID(c.in); got != c.want {
			t.Errorf("uselessHostID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
