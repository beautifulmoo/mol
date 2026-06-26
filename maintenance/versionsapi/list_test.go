package versionsapi

import "testing"

func TestPreviousVersionKeyFromEntries(t *testing.T) {
	key, err := PreviousVersionKeyFromEntries([]VersionEntry{
		{Version: "1.0.0", IsCurrent: true},
		{Version: "0.9.0", IsPrevious: true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "0.9.0" {
		t.Fatalf("key = %q, want 0.9.0", key)
	}

	_, err = PreviousVersionKeyFromEntries([]VersionEntry{
		{Version: "1.0.0", IsCurrent: true},
	})
	if err == nil {
		t.Fatal("expected error when no previous version")
	}
}

func TestCanRollbackFromEntries(t *testing.T) {
	if !CanRollbackFromEntries([]VersionEntry{
		{Version: "1.0.0", IsCurrent: true},
		{Version: "0.9.0", IsPrevious: true},
	}) {
		t.Fatal("want true when current != previous")
	}
	if CanRollbackFromEntries([]VersionEntry{
		{Version: "1.0.0", IsCurrent: true},
		{Version: "1.0.0", IsPrevious: true},
	}) {
		t.Fatal("want false when current == previous")
	}
	if CanRollbackFromEntries([]VersionEntry{
		{Version: "1.0.0", IsCurrent: true},
	}) {
		t.Fatal("want false when no previous")
	}
}
