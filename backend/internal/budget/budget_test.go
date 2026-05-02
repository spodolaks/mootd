package budget

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeReader stubs BudgetReader. Tests mutate dailyUSD between calls.
type fakeReader struct {
	mu       sync.Mutex
	dailyUSD float64
}

func (f *fakeReader) BudgetForUser(_ context.Context, userID string) (Cap, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Cap{UserID: userID, DailyUSD: f.dailyUSD, MonthlyUSD: f.dailyUSD * 15}, nil
}

// fakeTracker is an in-memory SpendTracker. Lets us inspect the
// Suspend call after a 200% breach without spinning up Redis.
type fakeTracker struct {
	mu        sync.Mutex
	spend     map[string]float64
	suspended map[string]bool
	suspendedUntil map[string]time.Time
}

func newFakeTracker() *fakeTracker {
	return &fakeTracker{
		spend:          map[string]float64{},
		suspended:      map[string]bool{},
		suspendedUntil: map[string]time.Time{},
	}
}

func (f *fakeTracker) TodaySpend(_ context.Context, userID string) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spend[userID], nil
}

func (f *fakeTracker) Increment(_ context.Context, userID string, costUSD float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spend[userID] += costUSD
	return nil
}

func (f *fakeTracker) IsSuspended(_ context.Context, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspended[userID], nil
}

func (f *fakeTracker) Suspend(_ context.Context, userID string, until time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspended[userID] = true
	f.suspendedUntil[userID] = until
	return nil
}

func TestEnforcer_AllowUnderCap(t *testing.T) {
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, newFakeTracker())
	d, _, err := e.Check(context.Background(), "u1", 0.10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Allow {
		t.Fatalf("expected Allow, got %v", d)
	}
}

func TestEnforcer_DenyOverCap(t *testing.T) {
	tr := newFakeTracker()
	tr.spend["u1"] = 0.95
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)
	d, reason, err := e.Check(context.Background(), "u1", 0.20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Deny {
		t.Fatalf("expected Deny (projected 1.15 > 1.00), got %v", d)
	}
	if reason.Code != "over_daily_cap" {
		t.Fatalf("expected code over_daily_cap, got %q", reason.Code)
	}
}

func TestEnforcer_Over200pct_AutoSuspends(t *testing.T) {
	tr := newFakeTracker()
	tr.spend["u1"] = 1.95 // already at 195% of $1 cap
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)
	d, reason, err := e.Check(context.Background(), "u1", 0.10) // projected 2.05 > 2.00
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Deny {
		t.Fatalf("expected Deny, got %v", d)
	}
	if reason.Code != "over_200pct" {
		t.Fatalf("expected code over_200pct, got %q", reason.Code)
	}
	// Tracker should now be suspended.
	suspended, _ := tr.IsSuspended(context.Background(), "u1")
	if !suspended {
		t.Fatal("expected user to be suspended after 200% breach")
	}
}

func TestEnforcer_AlreadySuspended(t *testing.T) {
	tr := newFakeTracker()
	tr.suspended["u1"] = true
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)
	d, reason, err := e.Check(context.Background(), "u1", 0.10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Deny {
		t.Fatalf("expected Deny, got %v", d)
	}
	if reason.Code != "auto_suspended" {
		t.Fatalf("expected code auto_suspended, got %q", reason.Code)
	}
}

func TestEnforcer_ZeroCap_AllowsThrough(t *testing.T) {
	// A 0 cap means "no cap configured" — don't deny everything.
	e := NewEnforcer(&fakeReader{dailyUSD: 0}, newFakeTracker())
	d, _, err := e.Check(context.Background(), "u1", 100.00)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Allow {
		t.Fatalf("expected Allow on zero cap, got %v", d)
	}
}

func TestEnforcer_NilSafe(t *testing.T) {
	var e *Enforcer
	d, _, err := e.Check(context.Background(), "u1", 0.10)
	if err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
	if d != Allow {
		t.Fatalf("expected Allow on nil enforcer, got %v", d)
	}
}
