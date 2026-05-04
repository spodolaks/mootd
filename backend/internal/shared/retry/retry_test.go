package retry

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDo_SuccessFirstTry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Options{MaxAttempts: 3, InitialDelay: time.Millisecond}, func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("got err %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestDo_RetriesOn5xx(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Options{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
	}, func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return HTTPErrorFor(503)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("got err %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestDo_DoesNotRetry4xx(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Options{MaxAttempts: 3, InitialDelay: time.Millisecond}, func(ctx context.Context) error {
		calls++
		return HTTPErrorFor(400)
	})
	if err == nil {
		t.Fatal("got nil err, want HTTPError")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (4xx is not retryable)", calls)
	}
}

func TestDo_ExhaustsAndReturnsLastErr(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Options{MaxAttempts: 2, InitialDelay: time.Millisecond}, func(ctx context.Context) error {
		calls++
		return HTTPErrorFor(502)
	})
	if err == nil {
		t.Fatal("got nil err, want HTTPError")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("err = %v, want last 502", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestDo_ContextCancelAborts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	err := Do(ctx, Options{
		MaxAttempts:  10,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
	}, func(ctx context.Context) error {
		calls++
		return HTTPErrorFor(500)
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if calls > 3 {
		t.Errorf("calls = %d, want few (cancel should abort early)", calls)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// Compile-time check that timeoutErr satisfies net.Error.
var _ net.Error = timeoutErr{}

func TestIsRetryable_TimeoutNetError(t *testing.T) {
	if !IsRetryable(timeoutErr{}) {
		t.Error("net.Error with Timeout()=true should be retryable")
	}
}

func TestIsRetryable_TransientSentinel(t *testing.T) {
	wrapped := errors.Join(errors.New("upstream"), ErrTransient)
	if !IsRetryable(wrapped) {
		t.Error("error wrapping ErrTransient should be retryable")
	}
}

func TestDo_OnRetryFires(t *testing.T) {
	hits := 0
	err := Do(context.Background(), Options{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     time.Millisecond,
		OnRetry: func(attempt int, _ error, _ time.Duration) {
			hits++
		},
	}, func(ctx context.Context) error {
		return HTTPErrorFor(500)
	})
	if err == nil {
		t.Fatal("expected err")
	}
	// 3 attempts → 2 retries → 2 OnRetry callbacks.
	if hits != 2 {
		t.Errorf("OnRetry hits = %d, want 2", hits)
	}
}
