package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimit returns middleware that limits requests per IP using a sliding window.
// maxRequests is the maximum number of requests allowed per window duration.
// The returned closer function stops the cleanup goroutine and should be called
// during server shutdown.
func RateLimit(maxRequests int, window time.Duration) (func(http.Handler) http.Handler, func()) {
	type visitor struct {
		count       int
		windowStart time.Time
	}

	var mu sync.Mutex
	visitors := make(map[string]*visitor)

	done := make(chan struct{})

	// Periodically clean up stale entries to prevent memory leaks.
	go func() {
		ticker := time.NewTicker(window * 2)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				now := time.Now()
				for ip, v := range visitors {
					if now.Sub(v.windowStart) > window {
						delete(visitors, ip)
					}
				}
				mu.Unlock()
			}
		}
	}()

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// clientIP honors X-Forwarded-For only from a trusted proxy,
			// so a direct caller can't spoof the header to dodge the limit.
			ip := r.RemoteAddr
			if cip := clientIP(r); cip != nil {
				ip = cip.String()
			}

			mu.Lock()
			now := time.Now()
			v, exists := visitors[ip]
			if !exists || now.Sub(v.windowStart) > window {
				visitors[ip] = &visitor{count: 1, windowStart: now}
				mu.Unlock()
				next.ServeHTTP(w, r)
				return
			}
			v.count++
			if v.count > maxRequests {
				mu.Unlock()
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}

	closer := func() { close(done) }
	return mw, closer
}
