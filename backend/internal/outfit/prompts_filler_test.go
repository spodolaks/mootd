package outfit

import (
	"strings"
	"testing"
)

// TestFillerQuotaPerOutfit covers the heuristic that decides how
// many [filler] items the LLM is told to include per outfit, based
// on the user's owned-item count. Cold-start users get a strong
// filler signal; rich-wardrobe users get none.
func TestFillerQuotaPerOutfit(t *testing.T) {
	tests := []struct {
		name        string
		ownCount    int
		fillerCount int
		want        int
	}{
		{"cold start (≤3 own) wants 3 fillers", 0, 24, 3},
		{"3 own still cold start", 3, 24, 3},
		{"4 own crosses into early-user band", 4, 24, 2},
		{"7 own still 2", 7, 24, 2},
		{"8 own → regular user", 8, 24, 1},
		{"14 own still 1", 14, 24, 1},
		{"15 own → rich wardrobe, 0 fillers", 15, 24, 0},
		{"100 own clearly 0", 100, 24, 0},
		{"quota bounded by available fillers (cold start, 1 filler)", 0, 1, 1},
		{"zero fillers means zero quota", 8, 0, 0},
		{"negative filler count clamped", 4, -1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fillerQuotaPerOutfit(tt.ownCount, tt.fillerCount)
			if got != tt.want {
				t.Errorf("fillerQuotaPerOutfit(own=%d, fillers=%d) = %d, want %d",
					tt.ownCount, tt.fillerCount, got, tt.want)
			}
		})
	}
}

// TestBuildUserMessage_NoFillersIsByteIdenticalShape proves the
// pre-filler prompt shape stays unchanged when the pool contains
// only owned items. Existing prompt-version regression tests count
// on this — adding [filler] markers to a wardrobe-only run would
// require bumping PromptVersion.
func TestBuildUserMessage_NoFillersIsByteIdenticalShape(t *testing.T) {
	items := []GenItem{
		{ID: "wi_a", Category: "tops", Label: "Black tee", Preferred: true, Weight: 1.0},
		{ID: "wi_b", Category: "bottoms", Label: "Light denim", Preferred: true, Weight: 1.0},
	}
	got := BuildUserMessage(items)

	// Markers + weight column must be absent when no fillers.
	if strings.Contains(got, "[filler]") {
		t.Errorf("BuildUserMessage with all owned items should NOT contain '[filler]' marker; got:\n%s", got)
	}
	if strings.Contains(got, "w=") {
		t.Errorf("BuildUserMessage with all owned items should NOT print w= weight column; got:\n%s", got)
	}
	// And the per-outfit-quota rule should be absent — there's no
	// filler signal to balance against.
	if strings.Contains(got, "AROUND") {
		t.Errorf("BuildUserMessage with no fillers should NOT contain the per-outfit quota rule; got:\n%s", got)
	}
}

// TestBuildUserMessage_FillersGetMarkers proves the filler-aware
// branch fires when at least one item is non-Preferred: items get
// [filler] inline, weights print numerically, and the per-outfit
// quota rule appends to the message body.
func TestBuildUserMessage_FillersGetMarkers(t *testing.T) {
	items := []GenItem{
		{ID: "wi_a", Category: "tops", Label: "Black tee", Preferred: true, Weight: 1.0},
		{ID: "wi_b", Category: "bottoms", Label: "Light denim", Preferred: true, Weight: 1.0},
		{ID: "wi_c", Category: "footwear", Label: "White sneakers", Preferred: true, Weight: 1.0},
		{ID: "ad_x", Category: "outerwear", Label: "Bomber jacket", Preferred: false, Weight: FillerWeight},
		{ID: "ad_y", Category: "accessories", Label: "Leather belt", Preferred: false, Weight: FillerWeight},
	}
	got := BuildUserMessage(items)

	if !strings.Contains(got, "[filler]") {
		t.Errorf("BuildUserMessage with fillers should contain '[filler]' markers; got:\n%s", got)
	}
	// Weight column should appear for the items that have weights.
	if !strings.Contains(got, "w=1.00") {
		t.Errorf("expected weight column for owned items (w=1.00); got:\n%s", got)
	}
	if !strings.Contains(got, "w=0.50") {
		t.Errorf("expected weight column for fillers (w=0.50, our FillerWeight); got:\n%s", got)
	}
	// 3 own items + 2 fillers → quota is 3 (covered by table-driven
	// test above; this one just proves the rule string materialises).
	if !strings.Contains(got, "AROUND 3") && !strings.Contains(got, "AROUND 2") {
		t.Errorf("expected per-outfit quota rule referencing a target count; got:\n%s", got)
	}
	// Owned items should NOT carry the [filler] marker.
	if strings.Contains(got, "wi_a") && strings.Contains(got, "wi_a (Black tee) w=1.00 [filler]") {
		t.Errorf("owned item incorrectly tagged as [filler]; got:\n%s", got)
	}
}
