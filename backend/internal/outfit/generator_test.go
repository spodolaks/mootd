package outfit

import (
	"math"
	"testing"
)

// Pin the mootd#67 mapping so a future tweak is a deliberate
// behaviour change reviewed in CR. Maps must stay continuous +
// monotonically increasing — the slider users see should never
// produce a smaller temperature than a value to its left.
func TestCreativityToTemperature_Mapping(t *testing.T) {
	cases := []struct {
		creativity, want float64
	}{
		{0, 0},          // sentinel: "no preference" → 0 → caller keeps default
		{0.01, 0.508},   // floor of the live range
		{0.25, 0.7},     // half-way in the conservative arc
		{0.5, 0.9},      // current default
		{0.75, 1.05},    // half-way in the high-variance arc
		{1.0, 1.2},      // ceiling
	}
	for _, c := range cases {
		got := CreativityToTemperature(c.creativity)
		if math.Abs(got-c.want) > 0.001 {
			t.Errorf("CreativityToTemperature(%.2f) = %.4f, want %.4f", c.creativity, got, c.want)
		}
	}
}

func TestCreativityToTemperature_Monotonic(t *testing.T) {
	prev := CreativityToTemperature(0.01)
	for c := 0.02; c <= 1.0; c += 0.01 {
		now := CreativityToTemperature(c)
		if now < prev {
			t.Fatalf("non-monotonic: at c=%.2f got %.4f after %.4f", c, now, prev)
		}
		prev = now
	}
}

func TestCreativityToTemperature_Clamps(t *testing.T) {
	if got := CreativityToTemperature(-0.5); got != 0 {
		t.Errorf("negative creativity should sentinel-return 0, got %v", got)
	}
	if got := CreativityToTemperature(2.0); math.Abs(got-1.2) > 0.001 {
		t.Errorf("over-1 creativity should clamp to 1 → 1.2 temp, got %v", got)
	}
}
