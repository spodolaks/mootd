package admin

import (
	"context"
	"time"
)

// DetectionRun is the wire shape for /admin/v1/detection-runs/{id}.
// Mirror of wardrobe.DetectionRun with a server-resolved userEmail
// + an inputImageUrl that the FE can hit through the admin API
// (rather than constructing GridFS paths directly).
type DetectionRun struct {
	ID                    string             `json:"id"`
	UserID                string             `json:"userId"`
	UserEmail             string             `json:"userEmail,omitempty"`
	CreatedAt             time.Time          `json:"createdAt"`
	DurationMs            int64              `json:"durationMs"`
	InputImageURL         string             `json:"inputImageUrl,omitempty"`
	InputImageContentType string             `json:"inputImageContentType,omitempty"`
	InputImageBytes       int64              `json:"inputImageBytes,omitempty"`
	OverallStyle          string             `json:"overallStyle,omitempty"`
	AnalyzeStats          map[string]any     `json:"analyzeStats,omitempty"`
	GenerateStats         map[string]any     `json:"generateStats,omitempty"`
	TotalCostUSD          float64            `json:"totalCostUsd,omitempty"`
	Items                 []DetectionRunItem `json:"items,omitempty"`

	// P1-10 (mootd-admin#15): admin-triggered re-runs.
	ParentRunID      string `json:"parentRunId,omitempty"`
	CreatedBy        string `json:"createdBy,omitempty"`
	DetectionVersion string `json:"detectionVersion,omitempty"`
}

// DetectionRunItem is one entry from the per-call generated_images
// archive. wardrobeItemId is the wardrobe_items _id this image was
// persisted as — when present, the FE constructs a thumbnail URL
// at /v1/wardrobe/items/{id}/image (public endpoint).
type DetectionRunItem struct {
	ItemType       string  `json:"itemType"`
	Category       string  `json:"category"`
	PromptUsed     string  `json:"promptUsed,omitempty"`
	RevisedPrompt  string  `json:"revisedPrompt,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
	WardrobeItemID string  `json:"wardrobeItemId,omitempty"`
}

// DetectionRunRepository is the admin's read surface for the
// archive. The production wiring is a thin adapter around
// wardrobe.DetectionRunMongoRepository — defined here so the admin
// package doesn't import wardrobe at compile time (one-way
// dependency, same as the outfit/wardrobe adapter pattern).
type DetectionRunRepository interface {
	FindRun(ctx context.Context, id string) (*DetectionRun, error)
	GetInputImage(ctx context.Context, runID string) ([]byte, string, error)

	// ListVersions returns the distinct, non-empty `detectionVersion`
	// labels persisted across detection_runs. Backs the dropdown in
	// the admin rerun modal — when empty (no rerun has set the field
	// yet), the UI falls back to a free-text input.
	ListVersions(ctx context.Context) ([]string, error)

	// Rerun replays the archived photo for `originalRunID` through
	// the detection pipeline and persists a child detection_runs row
	// with `parent_run_id` + `created_by` + `detection_version` set.
	// Returns the new (child) run ID. Observation-only — does NOT
	// save items to wardrobe_items (per the acceptance criteria on
	// mootd-admin#15).
	Rerun(ctx context.Context, originalRunID, adminID, detectionVersion string) (string, error)
}
