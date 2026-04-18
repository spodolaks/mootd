package outfit

import "net/http"

// Middleware is the standard middleware signature used across the codebase.
type Middleware = func(http.Handler) http.Handler

// RegisterRoutes registers outfit routes on mux. generateLimits is applied on
// top of authMiddleware for POST /v1/outfits/generate only — cost control for
// LLM-backed requests. Nil entries are skipped so the package stays usable when
// Redis is unavailable.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware Middleware, generateLimits ...Middleware) {
	mux.Handle("/v1/outfits", authMiddleware(http.HandlerFunc(h.Generate)))

	submit := http.Handler(http.HandlerFunc(h.SubmitGenerate))
	// Apply per-route rate limits in declaration order so the outermost one
	// (first arg) runs first — matches the way middleware is usually read.
	for i := len(generateLimits) - 1; i >= 0; i-- {
		if generateLimits[i] != nil {
			submit = generateLimits[i](submit)
		}
	}
	mux.Handle("/v1/outfits/generate", authMiddleware(submit))

	mux.Handle("/v1/outfits/jobs/", authMiddleware(http.HandlerFunc(h.PollJob)))
}
