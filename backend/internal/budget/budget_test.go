package budget

import (
	"context"
	"sync"
	"sync/atomic"
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
	mu             sync.Mutex
	spend          map[string]float64
	suspended      map[string]bool
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

func (f *fakeTracker) Reserve(_ context.Context, userID string, amount float64) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spend[userID] += amount
	return f.spend[userID], nil
}

func (f *fakeTracker) Release(_ context.Context, userID string, amount float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spend[userID] -= amount
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

func (f *fakeTracker) SuspendedUntil(_ context.Context, userID string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspendedUntil[userID], nil
}

func TestEnforcer_AllowUnderCap(t *testing.T) {
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, newFakeTracker())
	d, _, _, err := e.Check(context.Background(), "u1", 0.10)
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
	d, reason, _, err := e.Check(context.Background(), "u1", 0.20)
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
	d, reason, _, err := e.Check(context.Background(), "u1", 0.10) // projected 2.05 > 2.00
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
	// Suspended 23h ago for a 24h window → ~1h left, NOT a fresh +24h.
	stored := time.Now().UTC().Add(1 * time.Hour)
	tr.suspendedUntil["u1"] = stored
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)
	d, reason, _, err := e.Check(context.Background(), "u1", 0.10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Deny {
		t.Fatalf("expected Deny, got %v", d)
	}
	if reason.Code != "auto_suspended" {
		t.Fatalf("expected code auto_suspended, got %q", reason.Code)
	}
	// Must report the real stored expiry, not a fabricated now+24h.
	if reason.SuspendedUntil == nil {
		t.Fatal("expected SuspendedUntil to be populated")
	}
	if got := reason.SuspendedUntil.Sub(stored); got > time.Second || got < -time.Second {
		t.Errorf("SuspendedUntil should match stored value; off by %v", got)
	}
	if time.Until(*reason.SuspendedUntil) > 2*time.Hour {
		t.Errorf("expected ~1h remaining, got until=%v (fabricated +24h?)", reason.SuspendedUntil)
	}
}

func TestEnforcer_AlreadySuspended_FallbackWhenUntilUnknown(t *testing.T) {
	// Suspended flag set but no stored expiry (e.g. legacy key) → fall
	// back to the +24h upper bound rather than a zero timestamp.
	tr := newFakeTracker()
	tr.suspended["u1"] = true
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)
	_, reason, _, err := e.Check(context.Background(), "u1", 0.10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason.SuspendedUntil == nil || reason.SuspendedUntil.IsZero() {
		t.Fatal("expected a non-zero fallback SuspendedUntil")
	}
	if time.Until(*reason.SuspendedUntil) < 23*time.Hour {
		t.Errorf("expected ~24h fallback, got until=%v", reason.SuspendedUntil)
	}
}

func TestEnforcer_ZeroCap_AllowsThrough(t *testing.T) {
	// A 0 cap means "no cap configured" — don't deny everything.
	e := NewEnforcer(&fakeReader{dailyUSD: 0}, newFakeTracker())
	d, _, _, err := e.Check(context.Background(), "u1", 100.00)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Allow {
		t.Fatalf("expected Allow on zero cap, got %v", d)
	}
}

func TestEnforcer_ConcurrentChecksReserveAtomically(t *testing.T) {
	// Cap $1.00, each call reserves $0.60. Only ONE concurrent call can
	// fit; the rest must be denied. Pre-fix (read-then-check) every
	// goroutine read today=0, projected 0.60 < 1.00, and all passed —
	// the overspend race. With atomic Reserve exactly one wins.
	tr := newFakeTracker()
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)

	const goroutines = 20
	var allows int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // line them all up to maximise contention
			d, _, _, err := e.Check(context.Background(), "u1", 0.60)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if d == Allow {
				atomic.AddInt64(&allows, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if allows != 1 {
		t.Fatalf("expected exactly 1 Allow under contention, got %d", allows)
	}
	// The single winner's reservation should remain (0.60); all denied
	// callers must have refunded theirs.
	if got := tr.spend["u1"]; got < 0.59 || got > 0.61 {
		t.Errorf("expected ~0.60 reserved (one winner), got %.4f", got)
	}
}

func TestEnforcer_DeniedCallRefundsReservation(t *testing.T) {
	// A denied call must not leave its estimate reserved.
	tr := newFakeTracker()
	tr.spend["u1"] = 0.95
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)
	d, _, reserved, err := e.Check(context.Background(), "u1", 0.20) // 1.15 > 1.00 → deny
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != Deny {
		t.Fatalf("expected Deny, got %v", d)
	}
	if reserved != 0 {
		t.Errorf("denied call should report 0 reserved, got %.4f", reserved)
	}
	if got := tr.spend["u1"]; got < 0.94 || got > 0.96 {
		t.Errorf("reservation should have been refunded back to ~0.95, got %.4f", got)
	}
}

func TestEnforcer_AllowReservesAndReleaseRefunds(t *testing.T) {
	tr := newFakeTracker()
	e := NewEnforcer(&fakeReader{dailyUSD: 1.00}, tr)
	d, _, reserved, err := e.Check(context.Background(), "u1", 0.20)
	if err != nil || d != Allow {
		t.Fatalf("expected Allow, got d=%v err=%v", d, err)
	}
	if reserved != 0.20 {
		t.Fatalf("expected reserved 0.20, got %.4f", reserved)
	}
	if got := tr.spend["u1"]; got < 0.19 || got > 0.21 {
		t.Errorf("expected 0.20 reserved after Allow, got %.4f", got)
	}
	// Recorder books the actual, then the caller releases the estimate.
	_ = tr.Increment(context.Background(), "u1", 0.04) // actual cost
	_ = e.Release(context.Background(), "u1", reserved)
	if got := tr.spend["u1"]; got < 0.03 || got > 0.05 {
		t.Errorf("after release, spend should net to the ~0.04 actual, got %.4f", got)
	}
}

func TestEnforcer_NilSafe(t *testing.T) {
	var e *Enforcer
	d, _, _, err := e.Check(context.Background(), "u1", 0.10)
	if err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
	if d != Allow {
		t.Fatalf("expected Allow on nil enforcer, got %v", d)
	}
}
