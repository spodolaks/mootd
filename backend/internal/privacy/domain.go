// Package privacy implements GDPR-style data export and erasure
// across every collection that holds per-user data (P2-06 /
// mootd-admin#23).
//
// Two endpoints + one admin endpoint:
//
//   - DELETE /v1/privacy/self   — self-serve purge.
//   - GET    /v1/privacy/export — self-serve export (JSON ZIP).
//   - DELETE /admin/v1/users/{id}/purge — admin-driven purge,
//     gated by users:purge + MFA re-verification, audited.
//
// One Service orchestrates both flows so the cross-domain
// cleanup stays in one place. The user/ package's existing
// CascadeFn is left untouched (DELETE /v1/user/profile) — both
// flows produce equivalent end states; the privacy flow just
// also wipes admin-side observability collections (events,
// llm_calls, detection_runs).
package privacy

import "time"

// PurgeReport describes what was deleted in a purge. Returned
// to the caller and recorded on the audit row so we can answer
// "what got wiped?" later.
type PurgeReport struct {
	UserID      string           `json:"userId"`
	PurgedAt    time.Time        `json:"purgedAt"`
	Collections map[string]int64 `json:"collections"` // name → docs deleted
	Total       int64            `json:"total"`
}

// ExportData is the in-memory shape of a user export. Each
// field is a list of documents from one collection, kept as
// raw bson.M so we faithfully reproduce whatever the persistence
// layer holds — we don't want a privacy export that silently
// drops a field because the Go struct is out of date.
type ExportData struct {
	UserID         string    `json:"userId"`
	GeneratedAt    time.Time `json:"generatedAt"`
	User           any       `json:"user,omitempty"`
	WardrobeItems  []any     `json:"wardrobeItems,omitempty"`
	Outfits        []any     `json:"outfits,omitempty"`
	OutfitJobs     []any     `json:"outfitJobs,omitempty"`
	Moodboards     []any     `json:"moodboards,omitempty"`
	OutfitFeedback []any     `json:"outfitFeedback,omitempty"`
	Events         []any     `json:"events,omitempty"`
	LLMCalls       []any     `json:"llmCalls,omitempty"`
	DetectionRuns  []any     `json:"detectionRuns,omitempty"`
	UserBudget     any       `json:"userBudget,omitempty"`
}
