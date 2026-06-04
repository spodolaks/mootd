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

	keyA := buildCacheKey("user1", withFillersA, weather, nil)
	keyB := buildCacheKey("user1", withFillersB, weather, nil)
	keyOwnedOnly := buildCacheKey("user1", owned, weather, nil)

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
	k1 := buildCacheKey("user1", []wardrobe.ClothingItem{{ID: "own_a"}}, weather, nil)
	k2 := buildCacheKey("user1", []wardrobe.ClothingItem{{ID: "own_a"}, {ID: "own_b"}}, weather, nil)
	if k1 == k2 {
		t.Error("cache key should change when the owned-item set changes")
	}
}

// Sanity: the key is per-user (userID is part of the hash).
func TestBuildCacheKey_UserScoped(t *testing.T) {
	weather := Weather{Temperature: "12", Condition: "Cloudy", Unit: "C"}
	items := []wardrobe.ClothingItem{{ID: "own_a"}}
	if buildCacheKey("user1", items, weather, nil) == buildCacheKey("user2", items, weather, nil) {
		t.Error("cache key should be scoped per user")
	}
}
