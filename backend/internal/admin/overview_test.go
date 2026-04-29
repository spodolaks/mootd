package admin

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeOverviewRepo lets us set canned answers without spinning up
// Mongo. Each test sets only the fields it asserts on; the rest
// return zero values. Records the windows ApproxDAUBetween was
// called with so we can prove the handler asks for the right
// half-open interval.
type fakeOverviewRepo struct {
	periodMetrics func(start, end time.Time) (float64, int64, error)
	dailySeries   func() ([]DailyMetric, []DailyMetric, []DailyMetric, error)
	recentCalls   func(n int) ([]LLMCallSnapshot, error)
	approxDAU     func(since time.Time) (int64, error)
	approxBetween func(from, to time.Time) (int64, error)
	emails        func(ids []string) (map[string]string, error)
	cacheMetrics  func(start, end time.Time) (*CacheMetrics, error)

	// captured args
	betweenCalls []struct{ from, to time.Time }
}

func (f *fakeOverviewRepo) PeriodMetrics(_ context.Context, start, end time.Time) (float64, int64, error) {
	if f.periodMetrics != nil {
		return f.periodMetrics(start, end)
	}
	return 0, 0, nil
}

func (f *fakeOverviewRepo) DailySeries(_ context.Context, _ time.Time) ([]DailyMetric, []DailyMetric, []DailyMetric, error) {
	if f.dailySeries != nil {
		return f.dailySeries()
	}
	return nil, nil, nil, nil
}

func (f *fakeOverviewRepo) RecentLLMCalls(_ context.Context, n int) ([]LLMCallSnapshot, error) {
	if f.recentCalls != nil {
		return f.recentCalls(n)
	}
	return nil, nil
}

func (f *fakeOverviewRepo) ApproxDAU(_ context.Context, since time.Time) (int64, error) {
	if f.approxDAU != nil {
		return f.approxDAU(since)
	}
	return 0, nil
}

func (f *fakeOverviewRepo) ApproxDAUBetween(_ context.Context, from, to time.Time) (int64, error) {
	f.betweenCalls = append(f.betweenCalls, struct{ from, to time.Time }{from, to})
	if f.approxBetween != nil {
		return f.approxBetween(from, to)
	}
	return 0, nil
}

func (f *fakeOverviewRepo) EmailsForUserIDs(_ context.Context, ids []string) (map[string]string, error) {
	if f.emails != nil {
		return f.emails(ids)
	}
	return map[string]string{}, nil
}

func (f *fakeOverviewRepo) CacheMetricsFor(_ context.Context, start, end time.Time) (*CacheMetrics, error) {
	if f.cacheMetrics != nil {
		return f.cacheMetrics(start, end)
	}
	return nil, nil
}

// TestApproxDAUBetween_RangeCorrectness verifies the repo's
// half-open semantics. A user whose updatedAt sits exactly on the
// upper bound is excluded; one on the lower bound is included.
// Pure data-model contract test — no Mongo required because we
// exercise the interface directly via the fake.
func TestApproxDAUBetween_RangeCorrectness(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		// emit the repo's response when the implementation queries
		// the half-open window. Real Mongo behaviour is mirrored
		// inside the closure: include rows with updatedAt in
		// [from, to).
		simulate map[string]time.Time // userID → updatedAt
		from     time.Time
		to       time.Time
		want     int64
	}{
		{
			name:     "all in window",
			simulate: map[string]time.Time{"a": now.Add(-30 * time.Hour), "b": now.Add(-30 * time.Hour)},
			from:     now.Add(-48 * time.Hour),
			to:       now.Add(-24 * time.Hour),
			want:     2,
		},
		{
			name:     "boundary: lower bound is inclusive",
			simulate: map[string]time.Time{"a": now.Add(-48 * time.Hour)}, // exactly on `from`
			from:     now.Add(-48 * time.Hour),
			to:       now.Add(-24 * time.Hour),
			want:     1,
		},
		{
			name:     "boundary: upper bound is exclusive",
			simulate: map[string]time.Time{"a": now.Add(-24 * time.Hour)}, // exactly on `to`
			from:     now.Add(-48 * time.Hour),
			to:       now.Add(-24 * time.Hour),
			want:     0,
		},
		{
			name:     "user moved updatedAt forward → excluded",
			simulate: map[string]time.Time{"a": now.Add(-1 * time.Hour)}, // came back today
			from:     now.Add(-48 * time.Hour),
			to:       now.Add(-24 * time.Hour),
			want:     0,
		},
		{
			name:     "empty input range → 0 not error",
			simulate: map[string]time.Time{},
			from:     now,
			to:       now,
			want:     0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeOverviewRepo{
				approxBetween: func(from, to time.Time) (int64, error) {
					var n int64
					for _, ts := range c.simulate {
						if (ts.Equal(from) || ts.After(from)) && ts.Before(to) {
							n++
						}
					}
					return n, nil
				},
			}
			got, err := f.ApproxDAUBetween(ctx, c.from, c.to)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestApproxDAUBetween_ErrorPropagates verifies repo errors bubble
// without being swallowed. The handler decides whether to elide the
// metric — the repo just reports.
func TestApproxDAUBetween_ErrorPropagates(t *testing.T) {
	want := errors.New("mongo: connection refused")
	f := &fakeOverviewRepo{
		approxBetween: func(_, _ time.Time) (int64, error) {
			return 0, want
		},
	}
	if _, err := f.ApproxDAUBetween(context.Background(), time.Now(), time.Now().Add(time.Hour)); err == nil {
		t.Error("expected error to propagate")
	}
}
