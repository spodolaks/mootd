package admin

import (
	"testing"
	"time"
)

func TestTruncateToBucket_Day(t *testing.T) {
	in := time.Date(2026, 5, 3, 14, 23, 45, 0, time.UTC)
	got := truncateToBucket(in, "day")
	want := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("day truncate: got %v, want %v", got, want)
	}
}

func TestTruncateToBucket_Week(t *testing.T) {
	// 2026-05-03 is a Sunday. ISO week starts on Monday →
	// truncating Sunday should land on the previous Monday
	// (2026-04-27).
	in := time.Date(2026, 5, 3, 14, 23, 45, 0, time.UTC)
	got := truncateToBucket(in, "week")
	want := time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("week truncate of Sunday: got %v, want %v", got, want)
	}

	// Wednesday 2026-04-29 → also same Monday 2026-04-27.
	in2 := time.Date(2026, 4, 29, 9, 0, 0, 0, time.UTC)
	got2 := truncateToBucket(in2, "week")
	if !got2.Equal(want) {
		t.Fatalf("week truncate of Wed: got %v, want %v", got2, want)
	}

	// Monday 2026-04-27 → itself (idempotent).
	in3 := time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)
	got3 := truncateToBucket(in3, "week")
	if !got3.Equal(want) {
		t.Fatalf("week truncate of Monday: got %v, want %v", got3, want)
	}
}

func TestBucketIndex(t *testing.T) {
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		t    time.Time
		want int
	}{
		{start, 0},                                              // start itself
		{start.Add(2 * day), 2},                                 // 2 days in
		{start.Add(2*day + 5*time.Hour), 2},                     // mid-day rounds down
		{start.Add(-1 * time.Hour), -1},                         // before start
		{time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC), 5},        // 5 days
	}
	for _, c := range cases {
		got := bucketIndex(c.t, start, day)
		if got != c.want {
			t.Errorf("bucketIndex(%v): got %d, want %d", c.t, got, c.want)
		}
	}
}

func TestFormatBucketLabel(t *testing.T) {
	mon := time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)
	if got := formatBucketLabel(mon, "day"); got != "2026-04-27" {
		t.Errorf("day label: got %q, want %q", got, "2026-04-27")
	}
	// 2026-04-27 is in ISO week 18 of 2026.
	if got := formatBucketLabel(mon, "week"); got != "2026-W18" {
		t.Errorf("week label: got %q, want %q", got, "2026-W18")
	}
	// Single-digit week zero-pads.
	jan := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC) // Monday of W02
	if got := formatBucketLabel(jan, "week"); got != "2026-W02" {
		t.Errorf("zero-pad week: got %q, want %q", got, "2026-W02")
	}
}

func TestFormatISOWeek_ZeroPadding(t *testing.T) {
	cases := map[int]string{
		1:  "2026-W01",
		9:  "2026-W09",
		10: "2026-W10",
		52: "2026-W52",
	}
	for week, want := range cases {
		if got := formatISOWeek(2026, week); got != want {
			t.Errorf("formatISOWeek(2026, %d): got %q, want %q", week, got, want)
		}
	}
}
