package replcli

import "testing"

func TestSplitFields(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{`a b c`, []string{"a", "b", "c"}},
		{`apply-update self "/path/with spaces/bundle.tar.gz"`, []string{"apply-update", "self", "/path/with spaces/bundle.tar.gz"}},
		{`'single'`, []string{"single"}},
		{"", nil},
		{"  \t  ", nil},
	}
	for _, tc := range tests {
		got := splitFields(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("splitFields(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitFields(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
