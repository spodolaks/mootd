package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// LLMCall is one row in the llm_calls collection — the wide,
// denormalized hot-path table for cost + model + feature analytics.
//
// Field-level rationale lives in mootd-admin/docs/DATA_MODEL.md. The
// short version: feature is denormalised from the parent trace so
// "cost by feature" is a single-collection query, and prompt_version
// is denormalised from the running PromptVersion constant so we can
// filter feedback events by prompt without joining anything.
type LLMCall struct {
	ID               string    `bson:"_id"`
	TraceID          string    `bson:"traceId,omitempty"` // populated when the trace table lands (P1-03)
	UserID           string    `bson:"userId"`
	Provider         string    `bson:"provider"`
	Model            string    `bson:"model"`
	Feature          string    `bson:"feature"`
	InputTokens      int       `bson:"inputTokens"`
	OutputTokens     int       `bson:"outputTokens"`
	CacheReadTokens  int       `bson:"cacheReadTokens"`
	CacheWriteTokens int       `bson:"cacheWriteTokens"`
	DurationMs       int64     `bson:"durationMs"`
	Status           string    `bson:"status"` // "success" | "error" | "timeout"
	CostUSD          float64   `bson:"costUsd"`
	PromptHash       string    `bson:"promptHash,omitempty"`    // sha256 of system+user prompt; powers dedupe + "show every call with this prompt"
	PromptVersion    string    `bson:"promptVersion,omitempty"` // PromptVersion at call time
	ErrorMsg         string    `bson:"errorMsg,omitempty"`
	CreatedAt        time.Time `bson:"createdAt"`

	// Prompt archival (P1-11 / mootd-admin#16, Step B — inline storage).
	// Step C migrates these into a content-addressed prompt_snapshots
	// collection if storage growth becomes a real concern. Today they
	// live on the row directly because (a) it's simpler, (b) Mongo's
	// 16MB doc cap is enormous compared to our ~10KB-per-call payload,
	// and (c) it lets the future admin prompt-viewer (P1-12) be a
	// single-doc fetch.
	//
	// Each field is capped via truncate() so a pathological response
	// can't bloat the row past sane limits.
	SystemPrompt    string   `bson:"systemPrompt,omitempty"`
	UserMessage     string   `bson:"userMessage,omitempty"`
	ResponseRaw     string   `bson:"responseRaw,omitempty"`
	WardrobeItemIDs []string `bson:"wardrobeItemIds,omitempty"`
	// DetectionRunID stamps detection_* rows with the parent run id
	// so the admin trace-detail panel can fetch the input photo +
	// generated images via /admin/v1/detection-runs/{id}.
	// Empty for outfit-generation rows.
	DetectionRunID string `bson:"detectionRunId,omitempty"`

	// ReplayOf points back at the original llm_calls row when this
	// row is an admin-triggered replay (P3-03 / mootd-admin#26).
	// Empty on every row a real user generates. Used by the admin
	// trace-detail panel to render side-by-side prompt/response
	// diffs.
	ReplayOf string `bson:"replayOf,omitempty"`

	// Granular cost-tagging (mootd#63). Lets us slice "cost by
	// wardrobe size", "cost by image count", "cost by prompt
	// version" without re-scanning the prompt text. All optional —
	// older rows / non-outfit features can leave them at zero.
	WardrobeItemCount int `bson:"wardrobeItemCount,omitempty"`
	ImageCount        int `bson:"imageCount,omitempty"`        // 0 for non-vision providers
	RecentBoardCount  int `bson:"recentBoardCount,omitempty"`  // positive examples injected
	// SystemTokens / UserTokens / ResponseTokens split InputTokens
	// + OutputTokens into the three regions caller-supplied. The
	// existing InputTokens/OutputTokens stay as the totals.
	SystemTokens   int     `bson:"systemTokens,omitempty"`
	UserTokens     int     `bson:"userTokens,omitempty"`
	ResponseTokens int     `bson:"responseTokens,omitempty"`
	// CacheHitRatio = cacheRead / (cacheRead + cacheWrite +
	// inputTokens). Stored at write time so analytics can $group
	// without re-deriving on every read; the per-call inputs are
	// also there for callers that want to verify or recompute.
	CacheHitRatio float64 `bson:"cacheHitRatio,omitempty"`
}

// LLMCallRepository persists LLMCall rows. Reads come later (admin
// list/filter endpoints in P1-08); for now we only need Append so
// the hot path stays narrow.
type LLMCallRepository interface {
	AppendLLMCall(ctx context.Context, c LLMCall) error
}

// MongoLLMCallRepository is the production implementation. Stores
// rows in the llm_calls collection with the indexes specified in
// DATA_MODEL.md.
type MongoLLMCallRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoLLMCallRepository constructs the repo and ensures indexes.
func NewMongoLLMCallRepository(ctx context.Context, client *mongo.Client, dbName string) (*MongoLLMCallRepository, error) {
	r := &MongoLLMCallRepository{client: client, dbName: dbName}
	if err := r.ensureIndexes(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *MongoLLMCallRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("llm_calls")
}

func (r *MongoLLMCallRepository) ensureIndexes(ctx context.Context) error {
	_, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_user_created"),
		},
		{
			Keys: bson.D{{Key: "model", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_model_created"),
		},
		{
			Keys: bson.D{{Key: "feature", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_feature_created"),
		},
		{
			// Powers /admin/v1/traces?status=error filters + the
			// "show me every failed call this hour" admin query.
			// Tiny cardinality (success / error / timeout) but the
			// index lets Mongo skip non-matching rows on the hot
			// firehose path instead of scanning + filtering.
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_status_created"),
		},
		{
			Keys: bson.D{{Key: "promptVersion", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_promptversion_created"),
		},
		{
			// "Show me every call that used this prompt" — sparse so
			// pre-archival rows (no hash) don't bloat the index.
			Keys:    bson.D{{Key: "promptHash", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_prompthash_created").SetSparse(true),
		},
		{
			// Time-series scans — "everything from the last hour".
			Keys: bson.D{{Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_created_desc"),
		},
	})
	return err
}

// AppendLLMCall inserts one row. Errors propagate; the calling
// observability wrapper logs + swallows so a Mongo blip doesn't
// surface to the user as a failed outfit generation.
func (r *MongoLLMCallRepository) AppendLLMCall(ctx context.Context, c LLMCall) error {
	_, err := r.col().InsertOne(ctx, c)
	return err
}

// ── Helpers used by the wrapper ────────────────────────────────────

// HashPrompt returns the sha256 hex of a rendered prompt — used for
// content-addressed dedupe in P1-11 and for "show me every call
// that used this prompt" queries.
func HashPrompt(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// LogIfError is a small helper so the wrapper's call site reads
// linearly (the actual write is fire-and-forget from the request's
// perspective).
func LogIfError(logger *log.Logger, label string, err error) {
	if err != nil {
		logger.Printf("observability: %s: %v", label, err)
	}
}
