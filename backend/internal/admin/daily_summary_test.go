package admin

import (
	"strings"
	"testing"
	"time"
)

func TestNextDailyAtUTC_BeforeTarget(t *testing.T) {
	// 2026-05-04 03:00 UTC → next 07:00 should be same day.
	now := time.Date(2026, 5, 4, 3, 0, 0, 0, time.UTC)
	got := nextDailyAtUTC(now, 7)
	want := time.Date(2026, 5, 4, 7, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextDailyAtUTC_AfterTarget(t *testing.T) {
	// 2026-05-04 12:00 UTC → next 07:00 should roll to next day.
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	got := nextDailyAtUTC(now, 7)
	want := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextDailyAtUTC_AtTarget(t *testing.T) {
	// Exactly 07:00 — interpret as already fired, roll to tomorrow.
	now := time.Date(2026, 5, 4, 7, 0, 0, 0, time.UTC)
	got := nextDailyAtUTC(now, 7)
	want := time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseFounderEmails(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"a@x.com", []string{"a@x.com"}},
		{"a@x.com, b@y.com", []string{"a@x.com", "b@y.com"}},
		{"  a@x.com  ,  b@y.com  ", []string{"a@x.com", "b@y.com"}},
		{"a@x.com,a@x.com", []string{"a@x.com"}},                          // dedup
		{"a@x.com,A@X.COM", []string{"a@x.com"}},                          // dedup case-insensitive (first wins)
		{"b@y.com,a@x.com", []string{"a@x.com", "b@y.com"}},               // sorted
	}
	for _, c := range cases {
		got := ParseFounderEmails(c.in)
		if !equalStrings(got, c.want) {
			t.Errorf("ParseFounderEmails(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRenderDailySummaryText_HasAllSections(t *testing.T) {
	s := &DailySummary{
		WindowStart:   time.Date(2026, 5, 3, 7, 0, 0, 0, time.UTC),
		WindowEnd:     time.Date(2026, 5, 4, 7, 0, 0, 0, time.UTC),
		DAU:           42,
		Generations:   123,
		Errors:        2,
		SpendUSD:      1.45,
		PriorSpendUSD: 1.20,
		NewSignups:    7,
		TopUsersByCost: []DailyTopUser{
			{UserID: "u1", Email: "alice@example.com", CostUSD: 0.50, Calls: 10},
			{UserID: "u2", CostUSD: 0.20, Calls: 4},
		},
	}
	out := RenderDailySummaryText(s)
	for _, want := range []string{
		"Mootd daily summary",
		"DAU                  42",
		"LLM calls            123",
		"Errors               2",
		"New signups          7",
		"Spend                $1.45",
		"+21% vs prior 24h",
		"Top users by cost",
		"alice@example.com",
		"u2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderDailySummaryText_NoPriorSpendOmitsRatio(t *testing.T) {
	s := &DailySummary{
		WindowStart: time.Now(),
		WindowEnd:   time.Now(),
		SpendUSD:    1.0,
	}
	out := RenderDailySummaryText(s)
	if strings.Contains(out, "vs prior 24h") {
		t.Errorf("expected no prior ratio when prior=0, got: %s", out)
	}
}

func equalStrings(a, b []string) bool {
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
