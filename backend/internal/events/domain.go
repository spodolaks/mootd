// Package events implements the analytics-event ingest path
// (P2-02 / mootd-admin#19) and the read surface used by Phase 2's
// product-analytics views (funnels, retention, session aggregation).
//
// Time-series-friendly: writes append-only to one Mongo collection
// (`events`) with indexes geared to the typical reads —
// (userId, createdAt desc) and (name, createdAt desc) for
// per-user / per-name slices, (createdAt desc) for the firehose.
package events

import "time"

// Event is the canonical wire shape and the on-disk shape (we
// store what we receive, no transformation). One row per event.
//
// `Properties` is `map[string]any` rather than a typed struct on
// purpose: the catalog grows over time and the per-event
// property shapes evolve; modelling each one as a Go struct
// would force a backend redeploy on every catalog tweak. The
// validator below enforces the *set* of allowed names; the
// admin-side analyses inspect properties opportunistically.
type Event struct {
	ID         string         `bson:"_id"        json:"id,omitempty"`
	UserID     string         `bson:"userId"     json:"userId,omitempty"`
	SessionID  string         `bson:"sessionId"  json:"sessionId"`
	Name       string         `bson:"name"       json:"name"`
	Properties map[string]any `bson:"properties,omitempty" json:"properties,omitempty"`
	// CreatedAt is the server-assigned ingest time. The client
	// MAY supply `clientTs` in properties, but we don't trust
	// client clocks for any analysis — funnel windows + cohort
	// buckets all run off CreatedAt.
	CreatedAt time.Time `bson:"createdAt"  json:"createdAt"`
}

// IngestEvent is the per-event wire shape inside the batch
// POST. Strictly a subset of Event — we ignore any id /
// userId / createdAt the client tries to set.
type IngestEvent struct {
	Name       string         `json:"name"`
	SessionID  string         `json:"sessionId"`
	Properties map[string]any `json:"properties,omitempty"`
}

// IngestRequest is the batch wrapper. We keep it shallow for
// easier rate limiting + payload-size bounds (the existing
// middleware caps the body at 128KB, which is the issue's
// acceptance criterion).
type IngestRequest struct {
	Events []IngestEvent `json:"events"`
}

// EventValidationError represents one rejected event in the
// response. Index points back at the position in the request
// array so the client can correlate. The client doesn't get to
// retry the rejected one — bad data should be a code change in
// the SDK, not a server-side leniency we paper over.
type EventValidationError struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// IngestResponse echoes per-event outcome. Acceptance criterion
// from the issue: "Invalid events don't poison the batch —
// per-event validation errors returned in the response."
type IngestResponse struct {
	Accepted int                    `json:"accepted"`
	Rejected []EventValidationError `json:"rejected,omitempty"`
}

// CatalogNames is the canonical allowlist parsed once from
// mootd-contracts/events/schema.md. Hardcoded here rather than
// loaded at boot so a missing or corrupted catalog file doesn't
// take ingest down. When new events land in the catalog they
// must also land here — same coupling we accept for any other
// versioned schema (the alternative is a runtime-loaded list
// that drifts silently when ops forgets to vendor it).
//
// 18 events as of mootd-contracts/events/schema.md@2026-04.
var CatalogNames = map[string]bool{
	// Core flows
	"app_opened":  true,
	"screen_view": true,
	"session_end": true,

	// Session lifecycle (P2-03 — emitted by the SDK as part of
	// session tracking; stored alongside the rest)
	"session_start":     true,
	"session_heartbeat": true,

	// Wardrobe flows
	"photo_uploaded": true,
	"items_detected": true,
	"item_confirmed": true,
	"item_rejected":  true,

	// Outfit flows
	"generate_outfit_requested": true,
	"generated_outfit":          true,
	"viewed_outfit":             true,
	"rated_outfit":              true,
	"swapped_item":              true,

	// Moodboard / sharing
	"saved_moodboard":      true,
	"viewed_calendar_date": true,
	"shared_moodboard":     true,

	// Auth
	"signed_up":  true,
	"signed_in":  true,
	"signed_out": true,
}
