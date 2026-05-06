package wardrobe

import (
	"context"
	"errors"
	"fmt"
	"time"

	"mootd/backend/internal/shared/id"
)

// ErrRunNotFound is returned when a rerun is requested against a
// detection_runs row that doesn't exist (or whose archived photo is
// missing from GridFS — admins shouldn't have to distinguish between
// "row gone" and "image gone" — both render as 404 on the FE).
var ErrRunNotFound = errors.New("wardrobe: detection run not found")

// RerunDetection replays the archived photo from `originalRunID`
// through the detection pipeline and persists a child detection_runs
// row with `parent_run_id`, `created_by`, and `detection_version`
// set so the admin UI can render a side-by-side diff
// (mootd-admin#15 / P1-10).
//
// Observation-only: this does NOT call processDetected, so no
// wardrobe_items are written. The user's wardrobe is unchanged —
// the rerun lives entirely in the detection_runs collection.
//
// Storage: the archived image is also saved to GridFS under the
// child run's ID so the existing
// /admin/v1/detection-runs/{id}/input-image handler works without
// special-casing parent lookups. Reruns are admin-paced so the
// duplication cost is negligible (KBs per rerun, not MBs at scale).
//
// `detectionVersion` is forward-compatible — the upstream detection
// service is currently versionless, so the admin's chosen label is
// purely descriptive on this row. When the service starts versioning
// we'll honour it as a real override here.
func RerunDetection(
	ctx context.Context,
	detector DetectorBackend,
	repo *DetectionRunMongoRepository,
	originalRunID, adminID, detectionVersion string,
) (string, error) {
	if detector == nil {
		return "", errors.New("wardrobe: rerun requires a detector")
	}
	if repo == nil {
		return "", errors.New("wardrobe: rerun requires a detection_runs repo")
	}
	if originalRunID == "" {
		return "", errors.New("wardrobe: rerun requires an original run id")
	}

	orig, err := repo.FindRun(ctx, originalRunID)
	if err != nil {
		return "", fmt.Errorf("rerun lookup: %w", err)
	}
	if orig == nil {
		return "", ErrRunNotFound
	}

	imgBytes, contentType, err := repo.GetInputImage(ctx, originalRunID)
	if err != nil {
		return "", fmt.Errorf("rerun image fetch: %w", err)
	}
	if len(imgBytes) == 0 {
		return "", ErrRunNotFound
	}

	newID := id.Generate()
	startedAt := time.Now().UTC()
	detected, runData, err := detector.Detect(ctx, orig.UserID, newID, imgBytes, "rerun-"+originalRunID)
	if err != nil {
		return "", fmt.Errorf("rerun detect: %w", err)
	}
	endedAt := time.Now().UTC()

	// Build the items slice from jobItems — same shape persistDetectionRun
	// uses on the user-driven path.
	items := make([]DetectionRunItem, 0, len(detected))
	for _, d := range detected {
		items = append(items, DetectionRunItem{
			ItemType:   d.Type,
			Category:   d.Family,
			PromptUsed: d.PromptUsed,
			CostUSD:    d.GenerateCostUSD,
			// WardrobeItemID intentionally left empty — observation-only,
			// no items persisted to wardrobe.
		})
	}

	run := DetectionRun{
		ID:         newID,
		UserID:     orig.UserID,
		CreatedAt:  startedAt,
		DurationMs: endedAt.Sub(startedAt).Milliseconds(),
		Items:      items,

		// P1-10 metadata.
		ParentRunID:      originalRunID,
		CreatedBy:        adminID,
		DetectionVersion: detectionVersion,
	}
	if runData != nil {
		run.CreatedAt = runData.StartedAt
		run.DurationMs = runData.EndedAt.Sub(runData.StartedAt).Milliseconds()
		run.OverallStyle = runData.OverallStyle
		run.AnalyzeStats = runData.AnalyzeStats
		run.GenerateStats = runData.GenerateStats
		run.TotalCostUSD = runData.TotalCostUSD
	}

	if err := repo.SaveRun(ctx, run); err != nil {
		return "", fmt.Errorf("rerun save: %w", err)
	}
	if err := repo.SaveInputImage(ctx, newID, imgBytes, contentType); err != nil {
		// Best-effort: the run row is already saved with the diff
		// payload; a missing GridFS entry just means the FE can't
		// display the photo for the child run. Log + continue.
		// The next time SaveRun completes successfully we'll have a
		// row without an image — operator can re-trigger the rerun.
		return newID, fmt.Errorf("rerun image save: %w", err)
	}
	return newID, nil
}
