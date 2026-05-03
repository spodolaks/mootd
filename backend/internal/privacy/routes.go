package privacy

import "net/http"

// RegisterRoutes mounts the user-facing privacy endpoints on
// mux. The auth middleware is applied at the caller (app.go)
// rather than here so wiring stays consistent with the other
// /v1/* domains.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	mux.Handle("/v1/privacy/self", requireAuth(http.HandlerFunc(h.SelfPurge)))
	mux.Handle("/v1/privacy/export", requireAuth(http.HandlerFunc(h.SelfExport)))
}
