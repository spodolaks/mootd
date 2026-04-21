package wardrobe

import (
	"net/http"
	"strings"
)

// RegisterRoutes registers wardrobe routes on mux, protecting them with authMiddleware.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	// Sync detection — kept for callers running outside Cloudflare's 100s
	// edge timeout (direct backend hits, native apps talking to the origin
	// directly). The async path below is the default for web.
	mux.Handle("/v1/wardrobe/detect", authMiddleware(http.HandlerFunc(h.Detect)))

	// Async detection: POST submits, GET polls. Sharing the same /detect-jobs
	// prefix lets one HandleFunc dispatch both without pattern collisions.
	mux.HandleFunc("/v1/wardrobe/detect-jobs", authMiddleware(http.HandlerFunc(h.SubmitDetect)).ServeHTTP)
	mux.HandleFunc("/v1/wardrobe/detect-jobs/", authMiddleware(http.HandlerFunc(h.PollDetectJob)).ServeHTTP)

	mux.Handle("/v1/wardrobe/items", authMiddleware(http.HandlerFunc(h.Items)))
	// Image serving is public (item IDs are non-guessable UUIDs).
	// Must be registered before the auth-wrapped /v1/wardrobe/items/ catch-all.
	mux.HandleFunc("/v1/wardrobe/items/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/image"):
			h.ServeImage(w, r)
		case strings.HasSuffix(r.URL.Path, "/search"):
			authMiddleware(http.HandlerFunc(h.Search)).ServeHTTP(w, r)
		default:
			authMiddleware(http.HandlerFunc(h.Item)).ServeHTTP(w, r)
		}
	})
}
