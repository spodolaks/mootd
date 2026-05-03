package privacy

import "testing"

// userScopedCollections defines what a Purge wipes. This test
// asserts the allowlist matches what mootd-admin#23 promises.
// Anyone changing the list (adding a new user-scoped
// collection) must update both the constant + the issue.
//
// Listed alphabetically below for stable comparison; the actual
// allowlist preserves authoring order.
func TestUserScopedCollections_MatchesContract(t *testing.T) {
	want := map[string]string{
		"detection_runs":  "userId",
		"events":          "userId",
		"llm_calls":       "userId",
		"moodboards":      "userId",
		"outfit_feedback": "userId",
		"outfit_jobs":     "userId",
		"outfits":         "userId",
		"user_budgets":    "_id",
		"wardrobe_items":  "userId",
	}
	got := map[string]string{}
	for _, c := range userScopedCollections {
		got[c.Name] = c.Field
	}
	if len(got) != len(want) {
		t.Fatalf("user-scoped collection count: got %d (%v), want %d (%v)", len(got), keys(got), len(want), keys(want))
	}
	for name, field := range want {
		if got[name] != field {
			t.Errorf("collection %q: got field %q, want %q", name, got[name], field)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
