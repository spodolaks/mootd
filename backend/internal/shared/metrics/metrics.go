// Package metrics exposes Prometheus instruments + the
// /metrics scrape endpoint (mootd#39).
//
// Bind /metrics behind Caddy on the same host so it's reachable
// from a Prometheus scraper running alongside the backend but
// not from the public internet — the endpoint is unauthenticated
// because Prometheus doesn't speak our admin JWT format. Operator
// is expected to gate it at the front (Caddy basic auth or a
// firewall rule) when running multi-tenant.
//
// Three instruments worth standing up at the start:
//
//   - http_requests_in_flight (gauge)        — saturation signal
//   - http_request_duration_seconds (histogram) — latency by route
//   - retry_total{call,outcome} (counter)    — wired from
//     shared/retry.OnRetry to count recoveries vs exhaustions.
//
// Anything domain-specific (LLM call rate, queue depth, etc.)
// stays in its package and registers via prometheus.MustRegister
// at boot. This package only provides the shared instruments.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// httpRequestsInFlight is the gauge of currently-in-progress
// HTTP requests. Saturation indicator: a steadily-rising value
// means handlers are slower than incoming load.
var httpRequestsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "mootd",
	Subsystem: "http",
	Name:      "requests_in_flight",
	Help:      "Currently in-flight HTTP requests.",
})

// httpRequestDuration is the per-route latency histogram.
// Buckets cover the typical 10ms (cache hit) → 60s (LLM
// generation) range with logarithmic spacing.
var httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "mootd",
	Subsystem: "http",
	Name:      "request_duration_seconds",
	Help:      "End-to-end HTTP request duration.",
	Buckets:   []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
}, []string{"method", "route", "status"})

// RetryTotal counts retry attempts emitted by shared/retry's
// OnRetry hook. labels:
//
//	call    — short name of the outbound call ("anthropic",
//	          "openai", "rembg", "detection") — caller passes.
//	outcome — "retry" (will try again) or "exhausted" (final
//	          attempt failed). Wire `outcome=exhausted` from
//	          the caller after retry.Do returns an error.
var RetryTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "mootd",
	Subsystem: "outbound",
	Name:      "retry_total",
	Help:      "Outbound HTTP retries by call name and outcome.",
}, []string{"call", "outcome"})

// RedisStatus is the up/down gauge for Redis (mootd#55). 1 =
// reachable, 0 = unreachable. Updated by a background heartbeat
// in app.go; /readyz also consults Redis directly.
var RedisStatus = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "mootd",
	Subsystem: "redis",
	Name:      "up",
	Help:      "Redis up/down indicator (1=reachable, 0=unreachable).",
})

func init() {
	prometheus.MustRegister(
		httpRequestsInFlight,
		httpRequestDuration,
		RetryTotal,
		RedisStatus,
	)
}

// Handler returns the /metrics handler. Uses promhttp's default
// registry so domain packages can register their own collectors
// without further plumbing.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Instrument wraps an http.Handler with the in-flight gauge +
// per-request duration histogram. Apply once around the root
// router (NOT per route) so we get a single label set per
// request.
//
// `routeOf` extracts a low-cardinality route label from the
// request — usually `r.URL.Path` collapsed to the registered
// pattern (e.g. /v1/wardrobe/items/{id} not the actual id).
// Pass nil to label every request as `r.Method` only.
func Instrument(next http.Handler, routeOf func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		route := r.Method
		if routeOf != nil {
			route = routeOf(r)
			if route == "" {
				route = "_unknown"
			}
		}
		httpRequestDuration.
			WithLabelValues(r.Method, route, strconv.Itoa(rw.status)).
			Observe(time.Since(start).Seconds())
	})
}

// statusWriter mirrors the one in shared/middleware/logging.go
// so we can capture the final status code. Kept package-local
// to avoid an import cycle.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Flush forwards to the underlying writer's Flusher when one is
// available (mootd#62). The SSE handler casts the writer to
// http.Flusher; without this passthrough the wrap defeats the
// cast and the stream downgrades.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
