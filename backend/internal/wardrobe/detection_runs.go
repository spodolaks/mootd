package wardrobe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// DetectionRun is one archived submission to /v1/wardrobe/detect (P1-04 /
// mootd-admin#16). Contains everything an admin needs to debug a single
// run: the original photo (in GridFS), the per-item generated images
// (referenced via wardrobe_items so they share the existing image
// pipeline), the per-item prompts, and the full per-call cost breakdown.
//
// Indexed by (userId, createdAt desc) so the per-user detail tab can
// page through them. The trace-detail panel looks up by _id directly.
type DetectionRun struct {
	ID                    string                  `bson:"_id"`
	UserID                string                  `bson:"userId"`
	CreatedAt             time.Time               `bson:"createdAt"`
	DurationMs            int64                   `bson:"durationMs"`
	InputImageID          string                  `bson:"inputImageId,omitempty"`
	InputImageHash        string                  `bson:"inputImageHash,omitempty"`
	InputImageContentType string                  `bson:"inputImageContentType,omitempty"`
	InputImageBytes       int64                   `bson:"inputImageBytes,omitempty"`
	OverallStyle          string                  `bson:"overallStyle,omitempty"`
	AnalyzeStats          *detectionStats         `bson:"analyzeStats,omitempty"`
	GenerateStats         *detectionStats         `bson:"generateStats,omitempty"`
	TotalCostUSD          float64                 `bson:"totalCostUsd,omitempty"`
	Items                 []DetectionRunItem      `bson:"items,omitempty"`
}

// DetectionRunItem is one generated image inside a run. Mirrors the
// wire shape returned by /admin/v1/detection-runs/{id}.
type DetectionRunItem struct {
	ItemType        string  `bson:"itemType"        json:"itemType"`
	Category        string  `bson:"category"        json:"category"`
	PromptUsed      string  `bson:"promptUsed,omitempty"     json:"promptUsed,omitempty"`
	RevisedPrompt   string  `bson:"revisedPrompt,omitempty"  json:"revisedPrompt,omitempty"`
	CostUSD         float64 `bson:"costUsd,omitempty"        json:"costUsd,omitempty"`
	WardrobeItemID  string  `bson:"wardrobeItemId,omitempty" json:"wardrobeItemId,omitempty"`
}

// DetectionStatsToMap exposes a *detectionStats to outside packages
// (the admin adapter) without exporting the unexported internal
// type. Returns a nested map[string]any so callers don't depend on
// the exact field shape — the admin wire response forwards it
// untyped on purpose, since this region of the API mirrors whatever
// the upstream detection service publishes.
func DetectionStatsToMap(s *detectionStats) map[string]any {
	if s == nil {
		return nil
	}
	out := map[string]any{}
	if s.Claude != nil {
		out["claude"] = map[string]any{
			"model":         s.Claude.Model,
			"input_tokens":  s.Claude.InputTokens,
			"output_tokens": s.Claude.OutputTokens,
			"total_tokens":  s.Claude.TotalTokens,
			"cost_usd":      s.Claude.CostUSD,
		}
	}
	if s.OpenAIImages != nil {
		out["openai_images"] = map[string]any{
			"model":         s.OpenAIImages.Model,
			"input_tokens":  s.OpenAIImages.InputTokens,
			"output_tokens": s.OpenAIImages.OutputTokens,
			"total_tokens":  s.OpenAIImages.TotalTokens,
			"cost_usd":      s.OpenAIImages.CostUSD,
		}
	}
	if len(s.LocalModels) > 0 {
		locals := make([]map[string]any, 0, len(s.LocalModels))
		for _, lm := range s.LocalModels {
			locals = append(locals, map[string]any{
				"model_name":   lm.ModelName,
				"cpu_seconds":  lm.CPUSeconds,
				"wall_seconds": lm.WallSeconds,
			})
		}
		out["local_models"] = locals
	}
	if len(s.ModelsUsed) > 0 {
		out["models_used"] = s.ModelsUsed
	}
	if s.TotalWallSeconds > 0 {
		out["total_wall_seconds"] = s.TotalWallSeconds
	}
	return out
}

// DetectionRunRepository owns persistence for detection_runs +
// the original input photos in GridFS.
type DetectionRunRepository interface {
	SaveRun(ctx context.Context, run DetectionRun) error
	FindRun(ctx context.Context, id string) (*DetectionRun, error)
	SaveInputImage(ctx context.Context, runID string, data []byte, contentType string) error
	GetInputImage(ctx context.Context, runID string) ([]byte, string, error)
	SetWardrobeItemIDs(ctx context.Context, runID string, idsByItemType map[string]string) error
}

// DetectionRunMongoRepository implements DetectionRunRepository
// against the shared cluster. Uses a dedicated GridFS bucket
// (detection_inputs) to keep the original-photo blobs separate from
// the wardrobe-item bucket — different lifecycle (input photos are
// written once + read rarely; wardrobe items are read on every
// outfit generation).
type DetectionRunMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewDetectionRunMongoRepository constructs the repo and ensures
// indexes exist.
func NewDetectionRunMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*DetectionRunMongoRepository, error) {
	r := &DetectionRunMongoRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Per-user listing for the future user-detail Detection tab.
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("detection_runs_user_created"),
		},
		{
			// Time-range admin queries.
			Keys:    bson.D{{Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("detection_runs_created_desc"),
		},
	}); err != nil {
		return nil, fmt.Errorf("ensure detection_runs indexes: %w", err)
	}
	return r, nil
}

func (r *DetectionRunMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("detection_runs")
}

// inputBucket returns a dedicated GridFS bucket for raw detection
// input photos. Separate bucket name keeps the file metadata distinct
// from wardrobe-item images so eventual TTL / cleanup policies can
// run independently.
func (r *DetectionRunMongoRepository) inputBucket() *mongo.GridFSBucket {
	return r.client.Database(r.dbName).GridFSBucket(
		options.GridFSBucket().SetName("detection_inputs"),
	)
}

// SaveRun upserts a detection_run row by id. We use Upsert (not
// InsertOne) so the wardrobe handler can save the row early, then
// later patch in wardrobe item IDs once post-detection processing
// finishes. Idempotent on retry.
func (r *DetectionRunMongoRepository) SaveRun(ctx context.Context, run DetectionRun) error {
	if run.ID == "" {
		return errors.New("wardrobe: detection_run id required")
	}
	_, err := r.col().ReplaceOne(ctx, bson.M{"_id": run.ID}, run, options.Replace().SetUpsert(true))
	return err
}

// FindRun returns the row by id, or (nil, nil) when not found.
func (r *DetectionRunMongoRepository) FindRun(ctx context.Context, id string) (*DetectionRun, error) {
	var doc DetectionRun
	err := r.col().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// SaveInputImage writes the original photo bytes to GridFS keyed by
// runID. Called by the wardrobe handler after Detect() returns
// successfully — at that point the run row may already exist (the
// recorder fired during Detect) and we patch in the image id.
//
// On replace (rare — same runID submitted twice), we delete the old
// file first to keep storage bounded.
func (r *DetectionRunMongoRepository) SaveInputImage(ctx context.Context, runID string, data []byte, contentType string) error {
	bucket := r.inputBucket()

	// Delete any prior file under this runID for idempotency.
	cur, err := bucket.GetFilesCollection().Find(ctx, bson.M{"filename": runID})
	if err != nil {
		return fmt.Errorf("look up existing input image: %w", err)
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var existing struct {
			ID any `bson:"_id"`
		}
		if err := cur.Decode(&existing); err == nil && existing.ID != nil {
			_ = bucket.Delete(ctx, existing.ID)
		}
	}

	metadata := bson.D{{Key: "contentType", Value: contentType}}
	uploadOpts := options.GridFSUpload().SetMetadata(metadata)
	if err := bucket.UploadFromStreamWithID(ctx, runID, runID, bytes.NewReader(data), uploadOpts); err != nil {
		return fmt.Errorf("upload input image: %w", err)
	}

	// Patch the run row with the GridFS file id + dedupe hash.
	hash := sha256.Sum256(data)
	_, err = r.col().UpdateOne(ctx, bson.M{"_id": runID}, bson.M{"$set": bson.M{
		"inputImageId":          runID,
		"inputImageHash":        hex.EncodeToString(hash[:]),
		"inputImageContentType": contentType,
		"inputImageBytes":       int64(len(data)),
	}})
	return err
}

// GetInputImage reads the original input photo from GridFS.
func (r *DetectionRunMongoRepository) GetInputImage(ctx context.Context, runID string) ([]byte, string, error) {
	bucket := r.inputBucket()
	var meta struct {
		ID       any    `bson:"_id"`
		Metadata bson.M `bson:"metadata"`
	}
	err := bucket.GetFilesCollection().FindOne(ctx, bson.M{"filename": runID}).Decode(&meta)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, "", mongo.ErrFileNotFound
		}
		return nil, "", fmt.Errorf("look up input image: %w", err)
	}
	contentType := "image/jpeg"
	if v, ok := meta.Metadata["contentType"].(string); ok && v != "" {
		contentType = v
	}
	var buf bytes.Buffer
	if _, err := bucket.DownloadToStream(ctx, meta.ID, &buf); err != nil {
		return nil, "", fmt.Errorf("download input image: %w", err)
	}
	return buf.Bytes(), contentType, nil
}

// SetWardrobeItemIDs patches the items array on a run row, mapping
// each item's itemType+category to the wardrobe_items _id it ended
// up persisted as. Called by the wardrobe handler after the
// post-detection pipeline saves items to the user's wardrobe.
//
// idsByItemType is keyed by "itemType|category" to disambiguate when
// the user uploads a photo that produces, say, two pairs of shoes.
func (r *DetectionRunMongoRepository) SetWardrobeItemIDs(ctx context.Context, runID string, idsByItemType map[string]string) error {
	if len(idsByItemType) == 0 {
		return nil
	}
	// Read-modify-write: pull the current items, update each entry,
	// write back. Cheap (a handful of items per run) and avoids a
	// fancy aggregation update.
	run, err := r.FindRun(ctx, runID)
	if err != nil || run == nil {
		return err
	}
	for i := range run.Items {
		key := run.Items[i].ItemType + "|" + run.Items[i].Category
		if id, ok := idsByItemType[key]; ok {
			run.Items[i].WardrobeItemID = id
		}
	}
	_, err = r.col().UpdateOne(ctx, bson.M{"_id": runID}, bson.M{"$set": bson.M{"items": run.Items}})
	return err
}
