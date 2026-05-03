package events

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/id"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
)

// Handler implements POST /v1/events (P2-02 / mootd-admin#19).
//
// One handler today; future read-side endpoints (per-user
// recent activity, etc.) live alongside.
type Handler struct {
	logger *log.Logger
	repo   Repository
}

// NewHandler wires the dependencies. logger fallback is
// `log.Default()` so test setups that omit the field don't
// panic on the first ingest.
func NewHandler(logger *log.Logger, repo Repository) *Handler {
	if logger == nil {
		logger = log.Default()
	}
	return &Handler{logger: logger, repo: repo}
}

// RegisterRoutes wires the public endpoint with the per-user
// rate-limit middleware. The auth middleware is supplied by
// the caller — same pattern as the rest of the user-facing API.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler, rateLimit func(http.Handler) http.Handler) {
	chain := http.HandlerFunc(h.Ingest)
	var wrapped http.Handler = chain
	if rateLimit != nil {
		wrapped = rateLimit(wrapped)
	}
	if authMiddleware != nil {
		wrapped = authMiddleware(wrapped)
	}
	mux.Handle("/v1/events", wrapped)
}

// maxBatchBytes caps the request body. The issue's acceptance
// criterion is "Reject payloads > 128KB"; we enforce it via
// the standard MaxBytesReader pattern so the handler reads
// at most this much regardless of Content-Length headers.
const maxBatchBytes = 128 * 1024

// maxBatchEvents is a defensive event-count cap layered on top
// of the byte cap. 500 events × ~256 bytes each ≈ the byte
// cap; the count cap prevents pathological tiny-event spam
// from costing many small inserts.
const maxBatchEvents = 500

// Ingest handles POST /v1/events.
//
// Per-event validation: each event must have a known name
// and a non-empty sessionId. Rejects flow into the response's
// `rejected` array; valid events flow to the repo. Returns 200
// with the per-event outcome regardless — the issue's
// acceptance criterion is "invalid events don't poison the
// batch" + per-event errors are returned.
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Bound the request body before decoding — http's
	// MaxBytesReader enforces it on Read(), so a 200KB body
	// gets a precise "request body too large" instead of
	// running OOM.
	r.Body = http.MaxBytesReader(w, r.Body, maxBatchBytes)

	var req IngestRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		// http.MaxBytesError → 413; everything else 400.
		if strings.Contains(err.Error(), "request body too large") {
			response.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "payload exceeds 128KB"})
			return
		}
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(req.Events) == 0 {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "events array is required + non-empty"})
		return
	}
	if len(req.Events) > maxBatchEvents {
		response.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "more than 500 events in one batch"})
		return
	}

	now := time.Now().UTC()
	accepted := make([]Event, 0, len(req.Events))
	rejected := []EventValidationError{}
	for i, ev := range req.Events {
		if reason := validateEvent(ev); reason != "" {
			rejected = append(rejected, EventValidationError{Index: i, Reason: reason})
			continue
		}
		accepted = append(accepted, Event{
			ID:         id.Generate(),
			UserID:     userID,
			SessionID:  ev.SessionID,
			Name:       ev.Name,
			Properties: ev.Properties,
			CreatedAt:  now,
		})
	}

	if len(accepted) > 0 && h.repo != nil {
		// Bound persistence time so a Mongo blip can't sit on
		// the request indefinitely.
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := h.repo.AppendBatch(ctx, accepted); err != nil {
			h.logger.Printf("events: persist batch (%d) failed: %v", len(accepted), err)
			// Surface as 500 — the client should retry the whole
			// batch (idempotent on _id collisions).
			response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "ingest failed"})
			return
		}
	}

	response.WriteJSON(w, http.StatusOK, IngestResponse{
		Accepted: len(accepted),
		Rejected: rejected,
	})
}

// validateEvent returns a human-readable reason on failure or
// "" when the event is acceptable.
//
// Rules:
//   - Name must match the catalog (CatalogNames). Unknown
//     events log + reject so a misbehaving SDK doesn't pollute
//     storage.
//   - SessionId must be present. The catalog's invariants spell
//     out that every event carries one; the SDK is the source
//     of truth for generation.
//   - Properties optional. We don't validate the property
//     shape per-name today — that's enforced at the SDK layer
//     via TS types; the server treats properties as opaque
//     name-keyed bags.
func validateEvent(e IngestEvent) string {
	if e.Name == "" {
		return "missing name"
	}
	if !CatalogNames[e.Name] {
		return "unknown event name (not in catalog)"
	}
	if e.SessionID == "" {
		return "missing sessionId"
	}
	return ""
}
