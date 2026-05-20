package wardrobe

import (
	"reflect"
	"testing"
)

func TestFlattenTraits(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want map[string]string
	}{
		{
			name: "empty input returns nil",
			in:   map[string]any{},
			want: nil,
		},
		{
			name: "nil input returns nil",
			in:   nil,
			want: nil,
		},
		{
			name: "flat string fields pass through",
			in: map[string]any{
				"color":    "navy",
				"material": "wool",
			},
			want: map[string]string{"color": "navy", "material": "wool"},
		},
		{
			// The orchestrator returns the GarmentDescription
			// shape — each attribute is an object whose `primary`
			// holds the headline value. Before the fix this case
			// returned nil and the mobile import flow got stuck on
			// an empty form with a disabled Done button.
			name: "nested objects with primary key resolve to that leaf",
			in: map[string]any{
				"color":    map[string]any{"primary": "black", "secondary": "white"},
				"material": map[string]any{"primary": "leather"},
				"style":    map[string]any{"primary": "casual"},
				"occasion": map[string]any{"primary": "everyday"},
			},
			want: map[string]string{
				"color":    "black",
				"material": "leather",
				"style":    "casual",
				"occasion": "everyday",
			},
		},
		{
			// When `primary` is missing we still want to surface
			// something so the input renders pre-filled. Sorted-key
			// iteration makes the choice deterministic.
			name: "nested without primary falls back to first sorted leaf",
			in: map[string]any{
				"material": map[string]any{"fiber": "cotton", "weight": "midweight"},
			},
			want: map[string]string{"material": "cotton"},
		},
		{
			name: "deeply nested primary recurses",
			in: map[string]any{
				"color": map[string]any{
					"primary": map[string]any{"label": "burgundy"},
				},
			},
			want: map[string]string{"color": "burgundy"},
		},
		{
			name: "non-string scalars are dropped",
			in: map[string]any{
				"confidence": 0.92,
				"isFormal":   true,
				"color":      "red",
			},
			want: map[string]string{"color": "red"},
		},
		{
			name: "whitespace-only strings are dropped",
			in: map[string]any{
				"color":    "  ",
				"material": "wool",
			},
			want: map[string]string{"material": "wool"},
		},
		{
			name: "all attributes empty returns nil",
			in: map[string]any{
				"color":    map[string]any{},
				"material": "",
			},
			want: nil,
		},
		{
			// Closed-enum schema seen in the admin Claude
			// detector — flat string keys also need to keep
			// working unchanged.
			name: "admin-style flat schema still flattens",
			in: map[string]any{
				"color_primary":   "navy",
				"color_secondary": "white",
				"material":        "cotton twill",
				"fit":             "slim",
			},
			want: map[string]string{
				"color_primary":   "navy",
				"color_secondary": "white",
				"material":        "cotton twill",
				"fit":             "slim",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenTraits(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("flattenTraits(%#v) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
