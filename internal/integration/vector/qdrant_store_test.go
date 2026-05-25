package vector

import "testing"

func TestStablePointUUIDIsDeterministic(t *testing.T) {
	t.Parallel()

	left := stablePointUUID("summary:analysis_1")
	right := stablePointUUID("summary:analysis_1")
	other := stablePointUUID("summary:analysis_2")

	if left == "" {
		t.Fatalf("expected non-empty uuid")
	}
	if left != right {
		t.Fatalf("expected deterministic uuid, got %q and %q", left, right)
	}
	if left == other {
		t.Fatalf("expected different ids for different inputs")
	}
}
