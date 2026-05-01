package app

import (
	"context"
	"errors"

	"mootd/backend/internal/admin"
	"mootd/backend/internal/wardrobe"
)

// detectionRunAdapter satisfies admin.DetectionRunRepository on top
// of a wardrobe.DetectionRunMongoRepository. Lives in the app
// package (the wiring layer) so the admin package doesn't import
// wardrobe at compile time — same pattern as observability's
// outfit / wardrobe adapters.
//
// Carries a *wardrobe.Detector so admin-triggered re-runs (P1-10 /
// mootd-admin#15) can replay the archived photo without the admin
// package needing to know the detector exists.
type detectionRunAdapter struct {
	r        *wardrobe.DetectionRunMongoRepository
	detector *wardrobe.Detector
}

func newDetectionRunAdapter(r *wardrobe.DetectionRunMongoRepository, detector *wardrobe.Detector) *detectionRunAdapter {
	return &detectionRunAdapter{r: r, detector: detector}
}

func (a *detectionRunAdapter) FindRun(ctx context.Context, id string) (*admin.DetectionRun, error) {
	if a.r == nil {
		return nil, nil
	}
	doc, err := a.r.FindRun(ctx, id)
	if err != nil || doc == nil {
		return nil, err
	}
	return convertRun(doc), nil
}

func (a *detectionRunAdapter) GetInputImage(ctx context.Context, runID string) ([]byte, string, error) {
	if a.r == nil {
		return nil, "", nil
	}
	return a.r.GetInputImage(ctx, runID)
}

// ListVersions surfaces the distinct detection-version labels seen
// across detection_runs. Powers the rerun-modal dropdown on the
// admin UI.
func (a *detectionRunAdapter) ListVersions(ctx context.Context) ([]string, error) {
	if a.r == nil {
		return []string{}, nil
	}
	return a.r.ListDistinctDetectionVersions(ctx)
}

// Rerun replays the archived photo behind `originalRunID` through
// the detection service and writes a child detection_runs row.
// Returns the new run's ID (so the admin handler can echo it +
// 200 to the FE, which then GETs the new row to render the diff).
func (a *detectionRunAdapter) Rerun(ctx context.Context, originalRunID, adminID, detectionVersion string) (string, error) {
	if a.r == nil || a.detector == nil {
		return "", errors.New("admin: detection rerun not wired (missing repo or detector)")
	}
	return wardrobe.RerunDetection(ctx, a.detector, a.r, originalRunID, adminID, detectionVersion)
}

// convertRun maps the wardrobe-side struct to the admin wire shape.
// Stats are returned as map[string]any so the admin API can stream
// them through to the frontend without re-defining every nested
// shape — the FE renders them as a small key-value table.
func convertRun(doc *wardrobe.DetectionRun) *admin.DetectionRun {
	out := &admin.DetectionRun{
		ID:                    doc.ID,
		UserID:                doc.UserID,
		CreatedAt:             doc.CreatedAt,
		DurationMs:            doc.DurationMs,
		InputImageContentType: doc.InputImageContentType,
		InputImageBytes:       doc.InputImageBytes,
		OverallStyle:          doc.OverallStyle,
		TotalCostUSD:          doc.TotalCostUSD,

		// P1-10 metadata.
		ParentRunID:      doc.ParentRunID,
		CreatedBy:        doc.CreatedBy,
		DetectionVersion: doc.DetectionVersion,
	}
	if doc.AnalyzeStats != nil {
		out.AnalyzeStats = wardrobe.DetectionStatsToMap(doc.AnalyzeStats)
	}
	if doc.GenerateStats != nil {
		out.GenerateStats = wardrobe.DetectionStatsToMap(doc.GenerateStats)
	}
	for _, it := range doc.Items {
		out.Items = append(out.Items, admin.DetectionRunItem{
			ItemType:       it.ItemType,
			Category:       it.Category,
			PromptUsed:     it.PromptUsed,
			RevisedPrompt:  it.RevisedPrompt,
			CostUSD:        it.CostUSD,
			WardrobeItemID: it.WardrobeItemID,
		})
	}
	return out
}
