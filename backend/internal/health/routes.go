package health

import "net/http"

// RegisterRoutes registers the health check routes on mux.
//
// Three endpoints:
//   - /healthz    — liveness, always 200 if process is alive.
//   - /readyz     — readiness, 503 on Mongo unreachable.
//   - /v1/health  — public client-facing health (mootd#40).
//                   Carries version + minClientVersion +
//                   maintenance flag for the RN app to gate on.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.Healthz)
	mux.HandleFunc("/readyz", h.Readyz)
	mux.HandleFunc("/v1/health", h.ClientHealth)
}
