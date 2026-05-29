package admin

import "testing"

// #108 B2: path segments interpolated into the orchestrator proxy URL
// must reject traversal / injection, not just escape it (escaping
// doesn't stop ".." — it has no special characters).
func TestSafePathSegment(t *testing.T) {
	good := []string{
		"abc123",
		"6630f1a2b3c4d5e6f7081920", // 24-hex orchestrator id
		"source",                   // [a-z]+ bucket
		"sid_mask",
		"item-1_2",
	}
	for _, s := range good {
		if !safePathSegment(s) {
			t.Errorf("expected %q to be accepted", s)
		}
	}

	bad := []string{
		"",
		".",
		"..",
		"../etc",
		"a/b",
		"a/../b",
		"a%2Fb",  // encoded slash
		"a b",    // space
		"a?b",    // query injection
		"a#b",    // fragment
		"a.b",    // dot (would allow ".." style components elsewhere)
		"héllo",  // non-ascii
	}
	for _, s := range bad {
		if safePathSegment(s) {
			t.Errorf("expected %q to be rejected", s)
		}
	}
}
