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
	OutputTokens    int       `bson:"outputTokens"`
	CacheReadTokens  int       `bson:"cacheReadTokens"`
	CacheWriteTokens int       `bson:"cacheWriteTokens"`
	DurationMs       int64     `bson:"durationMs"`
	Status           string    `bson:"status"` // "success" | "error" | "timeout"
	CostUSD          float64   `bson:"costUsd"`
	PromptHash       string    `bson:"promptHash,omitempty"`     // sha256 of the rendered prompt; supports dedupe in P1-11
	PromptVersion    string    `bson:"promptVersion,omitempty"` // PromptVersion at call time
	ErrorMsg         string    `bson:"errorMsg,omitempty"`
	CreatedAt        time.Time `bson:"createdAt"`
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
			Keys: bson.D{{Key: "promptVersion", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("llm_calls_promptversion_created"),
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
