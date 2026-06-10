package archetype

import (
	"math"
	"testing"
)

const eps = 1e-9

func almostEqual(a, b float64) bool { return math.Abs(a-b) < eps }

// TestScoreItems_Basic exercises the scorer across representative wardrobes and
// asserts coarse, deterministic properties rather than brittle exact numbers:
// every archetype is scored, scores stay within [0,1], an empty wardrobe yields
// all zeros, and items with the right signals lift the matching archetype above
// an off-profile one.
func TestScoreItems_Basic(t *testing.T) {
	tests := []struct {
		name  string
		items []ItemTraits
		check func(t *testing.T, scores Scores)
	}{
		{
			name:  "empty wardrobe scores every archetype at zero",
			items: nil,
			check: func(t *testing.T, scores Scores) {
				if len(scores) != len(Profiles) {
					t.Fatalf("len(scores) = %d, want %d (one per profile)", len(scores), len(Profiles))
				}
				for name, s := range scores {
					if s != 0 {
						t.Errorf("archetype %q = %v, want 0 for empty wardrobe", name, s)
					}
				}
			},
		},
		{
			name: "ruler signals lift ruler above jester",
			items: []ItemTraits{
				{
					Category:     "outerwear",
					Color:        "charcoal",
					Fabric:       "wool",
					Style:        "structured tailored",
					Occasion:     "business",
					OverallStyle: "power",
					Details:      "investment piece",
				},
				{
					Category: "footwear",
					Color:    "black",
					Fabric:   "leather",
					Style:    "tailored",
					Occasion: "formal",
				},
			},
			check: func(t *testing.T, scores Scores) {
				if scores["ruler"] <= scores["jester"] {
					t.Errorf("ruler (%v) should outscore jester (%v) for tailored wool/leather wardrobe",
						scores["ruler"], scores["jester"])
				}
				if scores["ruler"] <= 0 {
					t.Errorf("ruler score = %v, want > 0", scores["ruler"])
				}
			},
		},
		{
			name: "all scores remain within the unit interval",
			items: []ItemTraits{
				{Category: "accessory", Color: "gold", ColorSecondary: "black", Fabric: "leather",
					Style: "structured power", Occasion: "formal business", OverallStyle: "authority",
					Details: "tailored investment"},
				{Category: "bag", Color: "burgundy", Fabric: "silk", Style: "luxe elegant",
					Occasion: "creative formal", OverallStyle: "refined", Details: "drape"},
			},
			check: func(t *testing.T, scores Scores) {
				for name, s := range scores {
					if s < 0 || s > 1 {
						t.Errorf("archetype %q score = %v, want within [0,1]", name, s)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := ScoreItems(tt.items)
			tt.check(t, scores)
		})
	}
}

// TestMerge covers the EMA-style blend across the alpha boundaries.
func TestMerge(t *testing.T) {
	existing := Scores{"ruler": 0.8, "rebel": 0.2}
	fresh := Scores{"ruler": 0.4, "rebel": 0.6}

	tests := []struct {
		name      string
		alpha     float64
		wantRuler float64
		wantRebel float64
	}{
		{
			// alpha=0 ignores the new scores entirely → existing wins.
			name: "alpha zero keeps existing", alpha: 0,
			wantRuler: 0.8, wantRebel: 0.2,
		},
		{
			// alpha=1 fully adopts the new scores.
			name: "alpha one adopts new", alpha: 1,
			wantRuler: 0.4, wantRebel: 0.6,
		},
		{
			// alpha=0.5 is the midpoint of the two inputs.
			name: "alpha half is the midpoint", alpha: 0.5,
			wantRuler: 0.6, wantRebel: 0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Merge(existing, fresh, tt.alpha)
			if !almostEqual(got["ruler"], tt.wantRuler) {
				t.Errorf("ruler = %v, want %v", got["ruler"], tt.wantRuler)
			}
			if !almostEqual(got["rebel"], tt.wantRebel) {
				t.Errorf("rebel = %v, want %v", got["rebel"], tt.wantRebel)
			}
			// Merge always spans the full profile set, even for keys absent
			// from both inputs (they blend from 0).
			if len(got) != len(Profiles) {
				t.Errorf("len(merged) = %d, want %d", len(got), len(Profiles))
			}
		})
	}
}

// TestMerge_MissingKeysTreatedAsZero confirms a key present in only one input
// blends against an implicit zero from the other.
func TestMerge_MissingKeysTreatedAsZero(t *testing.T) {
	existing := Scores{"sage": 1.0} // present only in existing
	fresh := Scores{"hero": 1.0}    // present only in fresh

	got := Merge(existing, fresh, 0.5)
	if !almostEqual(got["sage"], 0.5) {
		t.Errorf("sage = %v, want 0.5 (1.0 existing blended with 0 new)", got["sage"])
	}
	if !almostEqual(got["hero"], 0.5) {
		t.Errorf("hero = %v, want 0.5 (0 existing blended with 1.0 new)", got["hero"])
	}
}

// TestTopN covers the ordering contract plus the n boundary cases: n<=0 and
// n>=len both return the full sorted set, and ties don't drop entries.
func TestTopN(t *testing.T) {
	scores := Scores{
		"ruler":    0.9,
		"rebel":    0.5,
		"creator":  0.7,
		"sage":     0.1,
		"explorer": 0.5, // tie with rebel
	}

	t.Run("returns top n in descending order", func(t *testing.T) {
		got := TopN(scores, 2)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].Name != "ruler" {
			t.Errorf("got[0] = %q, want ruler (highest)", got[0].Name)
		}
		if got[1].Name != "creator" {
			t.Errorf("got[1] = %q, want creator (second highest)", got[1].Name)
		}
		if got[0].Score < got[1].Score {
			t.Errorf("not sorted descending: %v < %v", got[0].Score, got[1].Score)
		}
		// Titles are hydrated from Profiles.
		if got[0].Title != Profiles["ruler"].Title {
			t.Errorf("got[0].Title = %q, want %q", got[0].Title, Profiles["ruler"].Title)
		}
	})

	t.Run("n greater than len returns all", func(t *testing.T) {
		got := TopN(scores, 100)
		if len(got) != len(scores) {
			t.Fatalf("len = %d, want %d (all entries)", len(got), len(scores))
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].Score < got[i].Score {
				t.Fatalf("not sorted descending at %d: %v < %v", i, got[i-1].Score, got[i].Score)
			}
		}
	})

	t.Run("n zero returns all", func(t *testing.T) {
		got := TopN(scores, 0)
		if len(got) != len(scores) {
			t.Errorf("len = %d, want %d (n<=0 disables truncation)", len(got), len(scores))
		}
	})

	t.Run("n negative returns all", func(t *testing.T) {
		got := TopN(scores, -3)
		if len(got) != len(scores) {
			t.Errorf("len = %d, want %d (n<=0 disables truncation)", len(got), len(scores))
		}
	})

	t.Run("empty scores returns empty", func(t *testing.T) {
		got := TopN(Scores{}, 5)
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}
