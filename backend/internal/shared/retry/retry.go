// Package retry provides a shared exponential-backoff helper
// for outbound HTTP clients (mootd#44).
//
// Three patterns it standardises:
//
//   - Bounded attempts with exponential delay (jittered).
//   - Caller-controlled retry predicate via Options.RetryOn so
//     each generator can decide what's safe to retry. Default
//     retries 5xx HTTP statuses (returned as RetryableError) and
//     transient network errors (timeout / temporary).
//   - Context-aware: ctx cancellation aborts immediately, no
//     swallowed Done.
//
// Usage in a typical generator:
//
//	err := retry.Do(ctx, retry.Defaults, func(ctx context.Context) error {
//	    resp, err := client.Do(req.WithContext(ctx))
//	    if err != nil { return err }
//	    defer resp.Body.Close()
//	    if resp.StatusCode >= 500 {
//	        return retry.HTTPError(resp.StatusCode)
//	    }
//	    // ... read body
//	    return nil
//	})
//
// Retry decisions log at debug only — the caller knows which
// outcome it cares about. A future Prometheus counter
// (retry_total{call,outcome}) can hang off the OnRetry hook.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"time"
)

// Options governs a single Do() invocation.
type Options struct {
	// MaxAttempts is the total number of attempts (initial +
	// retries). Must be >= 1; defaults to 3.
	MaxAttempts int

	// InitialDelay is the delay before the second attempt.
	// Each subsequent attempt doubles the previous delay (cap
	// at MaxDelay). Defaults to 250ms.
	InitialDelay time.Duration

	// MaxDelay caps the exponential ramp. Defaults to 5s.
	MaxDelay time.Duration

	// Jitter is the proportion (0..1) of random spread added
	// to each delay. 0 = deterministic, 0.5 = ±50%. Defaults
	// to 0.25.
	Jitter float64

	// RetryOn classifies an error as retryable. When nil, the
	// default predicate retries HTTPError 5xx + transient net
	// errors.
	RetryOn func(error) bool

	// OnRetry, when set, is called *before* each retry sleep.
	// Useful for emitting metrics (mootd#39 retry_total
	// counter) without coupling this package to Prometheus.
	OnRetry func(attempt int, err error, delay time.Duration)
}

// Defaults is the zero-config Options most callers want:
// 3 attempts, 250ms → 500ms → up to 5s with ±25% jitter,
// default retry predicate.
var Defaults = Options{
	MaxAttempts:  3,
	InitialDelay: 250 * time.Millisecond,
	MaxDelay:     5 * time.Second,
	Jitter:       0.25,
}

// HTTPError wraps a status code so callers can `return
// retry.HTTPError(resp.StatusCode)` without hand-rolling an
// error type. The default RetryOn matches on this type.
type HTTPError struct {
	StatusCode int
}

// HTTPError returns an HTTPError as a Go error. Vet-friendly
// constructor: `if resp.StatusCode >= 500 { return
// retry.HTTPError(resp.StatusCode) }`.
//
//nolint:revive // intentional shadowing — package + type both named HTTPError
func HTTPErrorFor(status int) error {
	return &HTTPError{StatusCode: status}
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d", e.StatusCode)
}

// IsRetryable returns true for the default error classes:
//   - HTTPError with 5xx status
//   - net.Error with Timeout()
//   - context.DeadlineExceeded inside an inner request (NOT
//     the parent ctx — that aborts the loop)
//   - errors.Is(err, ErrTransient) for callers who want to
//     opt-in arbitrary errors.
//
// The parent ctx cancellation is handled separately in Do() so
// it always wins over RetryOn — no permission to retry once the
// caller has hung up.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500 && httpErr.StatusCode < 600
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Modern Go drops the Temporary() distinction; treat
		// any timeout as retryable (caller's outer ctx still
		// caps total time).
		return netErr.Timeout()
	}
	if errors.Is(err, ErrTransient) {
		return true
	}
	return false
}

// ErrTransient is a sentinel callers can wrap to opt arbitrary
// errors into the retry path: `return fmt.Errorf("upstream:
// %w", retry.ErrTransient)`.
var ErrTransient = errors.New("retry: transient")

// Do invokes fn up to opts.MaxAttempts times, sleeping
// between attempts with exponential backoff + jitter. Returns
// fn's last error on exhaustion, or ctx.Err() if the parent
// context is cancelled mid-flight.
//
// Single attempt + no retry: pass MaxAttempts=1 (e.g. when
// composing with another retry loop or in tests).
func Do(ctx context.Context, opts Options, fn func(context.Context) error) error {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = Defaults.MaxAttempts
	}
	if opts.InitialDelay == 0 {
		opts.InitialDelay = Defaults.InitialDelay
	}
	if opts.MaxDelay == 0 {
		opts.MaxDelay = Defaults.MaxDelay
	}
	if opts.RetryOn == nil {
		opts.RetryOn = IsRetryable
	}

	delay := opts.InitialDelay
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == opts.MaxAttempts || !opts.RetryOn(err) {
			return err
		}

		// Sleep with jitter. Capped at MaxDelay; clamped above
		// zero to avoid divide-by-zero on rand.Float64.
		sleep := jittered(delay, opts.Jitter)
		if opts.OnRetry != nil {
			opts.OnRetry(attempt, err, sleep)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
		delay *= 2
		if delay > opts.MaxDelay {
			delay = opts.MaxDelay
		}
	}
	return lastErr
}

// jittered returns d ± (jitter * d). When jitter is 0 the
// delay is returned unchanged.
func jittered(d time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return d
	}
	if jitter > 1 {
		jitter = 1
	}
	delta := float64(d) * jitter * (2*rand.Float64() - 1)
	out := time.Duration(float64(d) + delta)
	if out < 0 {
		out = 0
	}
	return out
}
