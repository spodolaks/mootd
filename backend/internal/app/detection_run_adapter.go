package app

import (
	"context"

	"mootd/backend/internal/admin"
	"mootd/backend/internal/wardrobe"
)

// detectionRunAdapter satisfies admin.DetectionRunRepository on top
// of a wardrobe.DetectionRunMongoRepository. Lives in the app
// package (the wiring layer) so the admin package doesn't import
// wardrobe at compile time — same pattern as observability's
// outfit / wardrobe adapters.
type detectionRunAdapter struct {
	r *wardrobe.DetectionRunMongoRepository
}

func newDetectionRunAdapter(r *wardrobe.DetectionRunMongoRepository) *detectionRunAdapter {
	return &detectionRunAdapter{r: r}
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
