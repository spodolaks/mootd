package outfit

import (
	"sort"
	"strings"
	"testing"
)

// TestParseLLMResponse covers the three JSON shapes the LLM might return.
// Each case asserts the normalized Items array so downstream code receives a
// flat list of wardrobe IDs regardless of how the model chose to structure
// its reply.
func TestParseLLMResponse(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantNames []string            // outfit names in order
		wantItems map[string][]string // outfit name → sorted item IDs
	}{
		{
			name: "standard outfits key with flat items",
			raw: `{
				"outfits": [
					{"name":"Weekend","items":["t1","b1","f1"],"description":"casual"},
					{"name":"Dinner","items":["t2","b2","f2"]}
				]
			}`,
			wantNames: []string{"Weekend", "Dinner"},
			wantItems: map[string][]string{
				"Weekend": {"b1", "f1", "t1"},
				"Dinner":  {"b2", "f2", "t2"},
			},
		},
		{
			name: "outfit_combinations key with object items",
			raw: `{
				"outfit_combinations": [
					{
						"outfit_name":"Studio",
						"description":"arty",
						"items":[{"id":"t1"},{"id":"b1"},{"id":"f1"},{"id":"a1"}]
					}
				]
			}`,
			wantNames: []string{"Studio"},
			wantItems: map[string][]string{
				"Studio": {"a1", "b1", "f1", "t1"},
			},
		},
		{
			name: "slot-based items with mixed object/string/array shapes",
			raw: `{
				"outfits": [
					{
						"name":"Slotted",
						"top":{"id":"t1"},
						"bottoms":"b1",
						"footwear":{"id":"f1"},
						"accessories":[{"id":"a1"},"a2"]
					}
				]
			}`,
			wantNames: []string{"Slotted"},
			wantItems: map[string][]string{
				"Slotted": {"a1", "a2", "b1", "f1", "t1"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLLMResponse(tc.raw)
			if err != nil {
				t.Fatalf("parseLLMResponse: %v", err)
			}
			if len(got) != len(tc.wantNames) {
				t.Fatalf("len(outfits) = %d, want %d", len(got), len(tc.wantNames))
			}
			for i, name := range tc.wantNames {
				if got[i].Name != name {
					t.Errorf("outfit[%d].Name = %q, want %q", i, got[i].Name, name)
				}
				sorted := append([]string(nil), got[i].Items...)
				sort.Strings(sorted)
				want := tc.wantItems[name]
				if !stringSliceEqual(sorted, want) {
					t.Errorf("outfit %q items = %v, want %v", name, sorted, want)
				}
			}
		})
	}
}

// TestParseLLMResponse_Malformed covers the error paths: invalid JSON and a
// well-formed object that doesn't match any of the known outfit-key shapes.
func TestParseLLMResponse_Malformed(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "invalid json",
			raw:     "not-json-at-all",
			wantErr: "unmarshal raw map",
		},
		{
			name:    "no recognized outfits key",
			raw:     `{"suggestions": ["foo"]}`,
			wantErr: "no recognized outfits key",
		},
		{
			name:    "outfits value is not an array",
			raw:     `{"outfits":"nope"}`,
			wantErr: "unmarshal outfits array",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLLMResponse(tc.raw)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestParseLLMResponse_StandardEmptyFallsThrough ensures that a standard
// {"outfits": [...]} shape whose items are all empty falls through to the
// generic parser so slot-based fields can still populate Items.
func TestParseLLMResponse_StandardEmptyFallsThrough(t *testing.T) {
	raw := `{
		"outfits": [
			{"name":"SlotFallback","items":[],"top":"t1","bottom":"b1","shoes":"f1"}
		]
	}`
	got, err := parseLLMResponse(raw)
	if err != nil {
		t.Fatalf("parseLLMResponse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	sorted := append([]string(nil), got[0].Items...)
	sort.Strings(sorted)
	want := []string{"b1", "f1", "t1"}
	if !stringSliceEqual(sorted, want) {
		t.Errorf("items = %v, want %v", sorted, want)
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
