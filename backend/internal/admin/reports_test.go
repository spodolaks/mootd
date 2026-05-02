package admin

import (
	"strings"
	"testing"
	"time"
)

func TestNextMonday0800UTC(t *testing.T) {
	// Sample known reference: 2026-05-04 is a Monday.
	monday := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)

	// Sunday 23:59 → next is Monday 08:00 same calendar day.
	sun := time.Date(2026, 5, 3, 23, 59, 0, 0, time.UTC)
	got := nextMonday0800UTC(sun)
	want := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Sunday 23:59 → %s, want %s", got, want)
	}

	// Monday 07:59 → 08:00 same day.
	morn := monday.Add(7*time.Hour + 59*time.Minute)
	got = nextMonday0800UTC(morn)
	if !got.Equal(want) {
		t.Errorf("Monday 07:59 → %s, want %s", got, want)
	}

	// Monday 08:00 (boundary) → next Monday.
	got = nextMonday0800UTC(want)
	if !got.Equal(want.AddDate(0, 0, 7)) {
		t.Errorf("Monday 08:00 → %s, want next Monday 08:00", got)
	}

	// Wednesday → next Monday 08:00.
	wed := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	got = nextMonday0800UTC(wed)
	if !got.Equal(time.Date(2026, 5, 11, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("Wednesday → %s, want 2026-05-11 08:00", got)
	}
}

func TestLastCompletedISOWeek(t *testing.T) {
	// Wed 2026-05-06: last completed week is 2026-04-27 (Mon) - 2026-05-04 (Mon exclusive).
	wed := time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)
	start, end := LastCompletedISOWeek(wed)
	if !start.Equal(time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("start: got %s, want 2026-04-27", start)
	}
	if !end.Equal(time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("end: got %s, want 2026-05-04", end)
	}

	// Mon 2026-05-04 noon: last completed is the prior week (Mon Apr 27 - Mon May 4).
	mon := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	start2, end2 := LastCompletedISOWeek(mon)
	if !start2.Equal(start) || !end2.Equal(end) {
		t.Errorf("Monday: got [%s, %s); want [%s, %s)", start2, end2, start, end)
	}

	// Sunday 2026-05-10 is mid-current-week (Mon May 4 → Mon May 11);
	// the most recently completed week is still the prior one.
	sun := time.Date(2026, 5, 10, 22, 0, 0, 0, time.UTC)
	start3, end3 := LastCompletedISOWeek(sun)
	if !start3.Equal(time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Sunday start: got %s, want 2026-04-27", start3)
	}
	if !end3.Equal(time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Sunday end: got %s, want 2026-05-04", end3)
	}
}

func TestParseISOWeekLabel(t *testing.T) {
	cases := []struct {
		label string
		ok    bool
	}{
		{"2026-W18", true},
		{"2026-W01", true},
		{"2026-W53", true},
		{"2026-W54", false},
		{"2026-W00", false},
		{"2026-18", false},
		{"foo", false},
		{"", false},
	}
	for _, c := range cases {
		_, err := ParseISOWeekLabel(c.label)
		if c.ok && err != nil {
			t.Errorf("%q: got error %v, want ok", c.label, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%q: got ok, want error", c.label)
		}
	}

	// Round-trip: build from a known Monday, label it, parse it back.
	monday := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	parsed, err := ParseISOWeekLabel(isoWeekLabel(monday))
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Equal(monday) {
		t.Errorf("round-trip: got %s, want %s", parsed, monday)
	}
}

func TestRenderWeeklyReportText_HappyPath(t *testing.T) {
	r := &WeeklyReport{
		WeekLabel:        "2026-W18",
		WeekStart:        time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC),
		WeekEnd:          time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		TotalCostUSD:     12.34,
		PriorWeekCostUSD: 10.00,
		DAU:              5,
		CostPerDAUUSD:    2.468,
		TopUsers: []WeeklyReportUserRow{
			{UserID: "u1", UserEmail: "alpha@example.com", CostUSD: 5.00, CallCount: 50},
			{UserID: "u2", CostUSD: 3.00, CallCount: 20},
		},
		ByModel: []WeeklyReportFacet{
			{Label: "claude-sonnet-4", CostUSD: 10.00, Share: 0.81, CallCount: 60},
			{Label: "gpt-4o-mini", CostUSD: 2.34, Share: 0.19, CallCount: 10},
		},
		ByFeature: []WeeklyReportFacet{
			{Label: "outfit_generate", CostUSD: 12.34, Share: 1.0, CallCount: 70},
		},
		Incidents:       []string{"Total spend up 23% WoW"},
		Recommendations: []string{"Review u1's flow"},
	}
	body := RenderWeeklyReportText(r)
	for _, want := range []string{
		"2026-W18",
		"$12.34",
		"$10.00",
		"DAU                  5",
		"alpha@example.com",
		"claude-sonnet-4",
		"outfit_generate",
		"Total spend up 23% WoW",
		"Review u1's flow",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestDetectIncidents_Spike(t *testing.T) {
	r := &WeeklyReport{
		TotalCostUSD:     20.00,
		PriorWeekCostUSD: 10.00,
	}
	out := detectIncidents(r)
	if len(out) == 0 {
		t.Fatal("expected an incident for 100% WoW spike")
	}
	if !strings.Contains(strings.Join(out, " "), "up") {
		t.Errorf("expected 'up' in incident text, got %v", out)
	}
}

func TestDetectIncidents_TopUserConcentration(t *testing.T) {
	r := &WeeklyReport{
		TotalCostUSD: 10.00,
		TopUsers: []WeeklyReportUserRow{
			{UserID: "whale", CostUSD: 6.00},
			{UserID: "u2", CostUSD: 1.00},
		},
	}
	out := detectIncidents(r)
	found := false
	for _, line := range out {
		if strings.Contains(line, "Single user accounts for 60%") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected single-user-concentration incident, got %v", out)
	}
}
