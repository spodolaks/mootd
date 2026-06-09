package outfit

import (
	"testing"

	"mootd/backend/internal/wardrobe"
)

// buildCacheKey must IGNORE archetype-default fillers (id prefix "ad_") so a
// small/cold-start wardrobe (which gets the most randomly-sampled fillers)
// still hits the cache instead of re-paying the LLM on every Generate.
func TestBuildCacheKey_IgnoresFillers(t *testing.T) {
	owned := []wardrobe.ClothingItem{{ID: "own_top"}, {ID: "own_bottom"}}
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}

	withFillersA := append(append([]wardrobe.ClothingItem{}, owned...),
		wardrobe.ClothingItem{ID: "ad_aaa"}, wardrobe.ClothingItem{ID: "ad_bbb"})
	withFillersB := append(append([]wardrobe.ClothingItem{}, owned...),
		wardrobe.ClothingItem{ID: "ad_ccc"}, wardrobe.ClothingItem{ID: "ad_ddd"})

	keyA := buildCacheKey("user1", withFillersA, weather, nil, 0.5, "claude")
	keyB := buildCacheKey("user1", withFillersB, weather, nil, 0.5, "claude")
	keyOwnedOnly := buildCacheKey("user1", owned, weather, nil, 0.5, "claude")

	if keyA != keyB {
		t.Errorf("cache key changed with a different filler sample: %s != %s", keyA, keyB)
	}
	if keyA != keyOwnedOnly {
		t.Errorf("key with fillers (%s) should equal the owned-only key (%s)", keyA, keyOwnedOnly)
	}
}

// Changing the user's OWNED items must still change the key.
func TestBuildCacheKey_OwnedItemsChangeKey(t *testing.T) {
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}
	k1 := buildCacheKey("user1", []wardrobe.ClothingItem{{ID: "own_a"}}, weather, nil, 0.5, "claude")
	k2 := buildCacheKey("user1", []wardrobe.ClothingItem{{ID: "own_a"}, {ID: "own_b"}}, weather, nil, 0.5, "claude")
	if k1 == k2 {
		t.Error("cache key should change when the owned-item set changes")
	}
}

// Sanity: the key is per-user (userID is part of the hash).
func TestBuildCacheKey_UserScoped(t *testing.T) {
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}
	items := []wardrobe.ClothingItem{{ID: "own_a"}}
	if buildCacheKey("user1", items, weather, nil, 0.5, "claude") == buildCacheKey("user2", items, weather, nil, 0.5, "claude") {
		t.Error("cache key should be scoped per user")
	}
}

// Identical inputs (including creativity + provider) must produce the SAME key
// so tapping "regenerate" twice under unchanged conditions hits the cache.
func TestBuildCacheKey_SameInputsSameKey(t *testing.T) {
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}
	items := []wardrobe.ClothingItem{{ID: "own_a"}, {ID: "own_b"}}
	k1 := buildCacheKey("user1", items, weather, nil, 0.5, "claude")
	k2 := buildCacheKey("user1", items, weather, nil, 0.5, "claude")
	if k1 != k2 {
		t.Errorf("identical inputs should produce identical keys: %s != %s", k1, k2)
	}
}

// Moving the creativity slider (mootd#67) must change the key so the user
// doesn't get the previous creativity's stale 24h-cached outfits (#154).
func TestBuildCacheKey_CreativityChangesKey(t *testing.T) {
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}
	items := []wardrobe.ClothingItem{{ID: "own_a"}}
	low := buildCacheKey("user1", items, weather, nil, 0.2, "claude")
	high := buildCacheKey("user1", items, weather, nil, 0.8, "claude")
	if low == high {
		t.Error("cache key should change when the creativity preference changes")
	}
}

// Creativity is BUCKETED to one decimal, so imperceptible float churn around
// the same slider position must NOT change the key (avoids cache churn).
func TestBuildCacheKey_CreativityBucketedStable(t *testing.T) {
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}
	items := []wardrobe.ClothingItem{{ID: "own_a"}}
	k1 := buildCacheKey("user1", items, weather, nil, 0.50, "claude")
	k2 := buildCacheKey("user1", items, weather, nil, 0.5000001, "claude")
	if k1 != k2 {
		t.Errorf("creativity within the same bucket should not change the key: %s != %s", k1, k2)
	}
}

// Switching the active generator/provider (OUTFIT_PROVIDER / model) must change
// the key so a provider swap doesn't serve the previous provider's cached
// outfits under an otherwise-identical key (#154).
func TestBuildCacheKey_ProviderChangesKey(t *testing.T) {
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}
	items := []wardrobe.ClothingItem{{ID: "own_a"}}
	claude := buildCacheKey("user1", items, weather, nil, 0.5, "claude")
	openai := buildCacheKey("user1", items, weather, nil, 0.5, "openai")
	if claude == openai {
		t.Error("cache key should change when the generator/provider name changes")
	}
}
