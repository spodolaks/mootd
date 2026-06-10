package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRouteLabel asserts the Prometheus route-label normaliser collapses
// per-id paths to a bounded set of labels (so the duration histogram doesn't
// emit one timeseries per item/job/user id) while leaving static routes and
// genuinely unknown paths recognisable.
func TestRouteLabel(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		// Static health/observability routes pass through unchanged.
		{"healthz passthrough", "/healthz", "/healthz"},
		{"readyz passthrough", "/readyz", "/readyz"},
		{"versioned health passthrough", "/v1/health", "/v1/health"},
		{"metrics passthrough", "/metrics", "/metrics"},

		// Per-id user routes collapse to a single bounded label.
		{"wardrobe item id", "/v1/wardrobe/items/abc123", "/v1/wardrobe/items/{id}"},
		{"wardrobe item id alt", "/v1/wardrobe/items/deadbeef", "/v1/wardrobe/items/{id}"},
		{"outfit job id", "/v1/outfits/jobs/job-42", "/v1/outfits/jobs/{id}"},
		{"moodboard id", "/v1/moodboards/mb_99", "/v1/moodboards/{id}"},
		{"surface id", "/v1/surfaces/panel-7", "/v1/surfaces/{id}"},

		// Per-id admin routes likewise collapse, including the {name} variant.
		{"admin user id", "/admin/v1/users/u123", "/admin/v1/users/{id}"},
		{"admin trace id", "/admin/v1/traces/t-1", "/admin/v1/traces/{id}"},
		{"admin prompt name", "/admin/v1/prompts/outfit-v3", "/admin/v1/prompts/{name}"},

		// An unmatched path past the third segment collapses to its prefix +
		// wildcard so a brand-new route can't blow up cardinality.
		{"unknown deep route", "/v1/widgets/xyz/extra", "/v1/widgets/xyz/*"},

		// A short unmatched path has no per-id segment to bound, so it is
		// returned verbatim.
		{"unknown shallow route", "/v1/whoami", "/v1/whoami"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if got := routeLabel(r); got != tt.want {
				t.Errorf("routeLabel(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
