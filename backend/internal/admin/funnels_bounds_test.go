package admin

import (
	"testing"
	"time"
)

// #110 E3: the step query must be time-bounded by [min, max] anchor so
// it doesn't scan all-history. anchorBounds computes that bound.
func TestAnchorBounds(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	anchors := map[string]time.Time{
		"u1": base,
		"u2": base.Add(2 * time.Hour),
		"u3": base.Add(-1 * time.Hour),
	}
	ids, minA, maxA := anchorBounds(anchors)
	if len(ids) != 3 {
		t.Fatalf("expected 3 ids, got %d", len(ids))
	}
	if !minA.Equal(base.Add(-1 * time.Hour)) {
		t.Errorf("min: got %v, want %v", minA, base.Add(-1*time.Hour))
	}
	if !maxA.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("max: got %v, want %v", maxA, base.Add(2*time.Hour))
	}
}

func TestAnchorBounds_Empty(t *testing.T) {
	ids, minA, maxA := anchorBounds(map[string]time.Time{})
	if len(ids) != 0 {
		t.Errorf("expected no ids, got %d", len(ids))
	}
	if !minA.IsZero() || !maxA.IsZero() {
		t.Errorf("expected zero times for empty input, got min=%v max=%v", minA, maxA)
	}
}

func TestAnchorBounds_Single(t *testing.T) {
	at := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	ids, minA, maxA := anchorBounds(map[string]time.Time{"only": at})
	if len(ids) != 1 || ids[0] != "only" {
		t.Fatalf("expected [only], got %v", ids)
	}
	if !minA.Equal(at) || !maxA.Equal(at) {
		t.Errorf("single anchor should be both min and max; got min=%v max=%v", minA, maxA)
	}
}
