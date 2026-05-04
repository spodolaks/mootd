// Package endpoints provides a tiny round-robin pool over a
// comma-separated list of upstream URLs (mootd#56).
//
// Usage:
//
//	pool := endpoints.NewPool(os.Getenv("DETECTION_API_BASE_URL"))
//	url := pool.Next()       // round-robin
//	url := pool.Fallback(url) // skip the URL that just failed
//
// Phase-1 scope: per-call rotation + skip-failed-URL helper.
// Health checks, weighted routing, and proper service discovery
// (Consul / Nomad / k8s endpoints) are out of scope.
package endpoints

import (
	"strings"
	"sync/atomic"
)

// Pool is a goroutine-safe round-robin over a fixed list of
// URLs. Empty input → Pool with a single empty string, which
// callers should treat as "not configured".
type Pool struct {
	urls []string
	idx  atomic.Uint64
}

// NewPool parses a comma-separated list of URLs. Trims
// whitespace, drops empty entries, deduplicates while
// preserving order.
func NewPool(commaSeparated string) *Pool {
	seen := map[string]struct{}{}
	urls := []string{}
	for _, raw := range strings.Split(commaSeparated, ",") {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	if len(urls) == 0 {
		urls = []string{""}
	}
	return &Pool{urls: urls}
}

// Next returns the next URL in round-robin order. Goroutine-safe.
func (p *Pool) Next() string {
	if len(p.urls) == 1 {
		return p.urls[0]
	}
	i := p.idx.Add(1) - 1
	return p.urls[int(i%uint64(len(p.urls)))]
}

// Fallback returns a URL different from `failed`, or the same
// URL when the pool has only one entry. Useful inside a retry
// loop to skip the host that just 5xx'd.
//
// Round-robin progresses normally, so a single failed call
// doesn't bias the load distribution.
func (p *Pool) Fallback(failed string) string {
	for i := 0; i < len(p.urls); i++ {
		next := p.Next()
		if next != failed {
			return next
		}
	}
	// Single-entry pool, or every entry equals `failed`.
	return p.urls[0]
}

// Size returns the configured pool size. Useful for log lines
// and health checks.
func (p *Pool) Size() int {
	if len(p.urls) == 1 && p.urls[0] == "" {
		return 0
	}
	return len(p.urls)
}

// All returns the configured URLs in declaration order. Read-
// only — callers must not mutate the result.
func (p *Pool) All() []string {
	return p.urls
}
