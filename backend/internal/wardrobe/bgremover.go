package wardrobe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	"mootd/backend/internal/shared/metrics"
	"mootd/backend/internal/shared/retry"
)

// BackgroundRemover calls an external service to remove image backgrounds.
//
// http.Post (used by an earlier version) goes through http.DefaultClient
// which has no Timeout — a hung rembg service silently held goroutines
// for the full request duration. Bounded client guards the worker pool.
//
// Single-attempt requests retry once on a 5xx via the shared
// retry helper (mootd#44). The rembg service occasionally
// throws a 502 mid-load; one retry with 250ms backoff clears
// it without surfacing a user-facing failure.
type BackgroundRemover struct {
	baseURL string
	client  *http.Client
}

// bgRemoverTimeout caps the round-trip to rembg. Real-world bg-remove
// of a single 1024×1024 photo takes ~3-8s on the local CPU service;
// 60s is generous headroom without leaving the worker hung on a
// silent failure.
const bgRemoverTimeout = 60 * time.Second

// NewBackgroundRemover creates a BackgroundRemover pointing at baseURL.
func NewBackgroundRemover(baseURL string) *BackgroundRemover {
	return &BackgroundRemover{
		baseURL: baseURL,
		client:  &http.Client{Timeout: bgRemoverTimeout},
	}
}

// RemoveBackground sends imageData to the service and returns the PNG result.
// filename is used as the multipart filename (e.g. "item.jpg").
func (b *BackgroundRemover) RemoveBackground(imageData []byte, filename string) ([]byte, error) {
	// Build the multipart body once; reused across retry
	// attempts. Each attempt re-reads from a fresh
	// bytes.Reader — body needs to be replayable.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("files", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(imageData); err != nil {
		return nil, fmt.Errorf("write image data: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}
	contentType := mw.FormDataContentType()
	bodyBytes := body.Bytes()

	// Outer caller doesn't pass a context — use a per-call
	// budget anchored on the client's Timeout. Retry budget is
	// 2 attempts (initial + 1 retry) with 250ms backoff; rembg
	// 5xx is intermittent and usually clears on the first
	// retry. Keep MaxAttempts low so a deeply-broken rembg
	// fails fast and the caller surfaces a UX message.
	ctx, cancel := context.WithTimeout(context.Background(), 2*bgRemoverTimeout)
	defer cancel()

	var png []byte
	err = retry.Do(ctx, retry.Options{
		MaxAttempts:  2,
		InitialDelay: 250 * time.Millisecond,
		MaxDelay:     time.Second,
		OnRetry: func(attempt int, retryErr error, delay time.Duration) {
			log.Printf("bgremover: attempt %d failed: %v — retrying in %s", attempt, retryErr, delay)
			// mootd#39 — bump the retry_total counter so
			// /metrics surfaces this as
			// retry_total{call="rembg",outcome="retry"}.
			// On final failure (after retry.Do returns)
			// we'd emit outcome=exhausted; rembg's small
			// retry budget makes that path noisy enough
			// to not bother gating yet.
			metrics.RetryTotal.WithLabelValues("rembg", "retry").Inc()
		},
	}, func(ctx context.Context) error {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost,
			b.baseURL+"/remove-background", bytes.NewReader(bodyBytes)) //nolint:gosec // URL from trusted config
		if reqErr != nil {
			return fmt.Errorf("build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", contentType)
		resp, doErr := b.client.Do(req)
		if doErr != nil {
			return doErr
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			// 5xx → retry.HTTPError surfaces, retry kicks in.
			// 4xx → bare error returns, retry exits.
			if resp.StatusCode >= 500 {
				return retry.HTTPErrorFor(resp.StatusCode)
			}
			return fmt.Errorf("bg remover returned %d: %s", resp.StatusCode, string(errBody))
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxImageSize))
		if readErr != nil {
			return fmt.Errorf("read bg remover response: %w", readErr)
		}
		png = body
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("bg remover: %w", err)
	}
	return png, nil
}
