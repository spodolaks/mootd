package metrics

import (
	"net/http"
	"testing"
)

func TestBoundRouteLabel(t *testing.T) {
	tests := []struct {
		name   string
		route  string
		status int
		want   string
	}{
		{"404 collapses an unmatched scanner path", "/.env", http.StatusNotFound, notFoundRoute},
		{"404 collapses /wp-login.php", "/wp-login.php", http.StatusNotFound, notFoundRoute},
		{"404 on a real templated route also collapses", "/v1/wardrobe/items/{id}", http.StatusNotFound, notFoundRoute},
		{"200 keeps the route label", "/v1/wardrobe/items/{id}", http.StatusOK, "/v1/wardrobe/items/{id}"},
		{"405 keeps the route label", "/v1/outfits", http.StatusMethodNotAllowed, "/v1/outfits"},
		{"500 keeps the route label", "/v1/outfits", http.StatusInternalServerError, "/v1/outfits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boundRouteLabel(tt.route, tt.status); got != tt.want {
				t.Errorf("boundRouteLabel(%q, %d) = %q, want %q", tt.route, tt.status, got, tt.want)
			}
		})
	}
}
