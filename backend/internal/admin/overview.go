package admin

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// OverviewMetrics is the payload of GET /admin/v1/overview. The
// numbers here power the dashboard's three KPI cards. Phase 1 cut:
// today's spend + LLM call count + DAU from the users collection's
// updatedAt heuristic.
//
// LastNCalls is the recent-activity feed at the bottom of the page —
// not a full /traces firehose (that's #13), just the last 10 calls so
// the dashboard isn't a wall of zeros on a fresh deploy.
type OverviewMetrics struct {
	SpendUsdToday      float64           `json:"spendUsdToday"`
	CallCountToday     int64             `json:"callCountToday"`
	DauApprox          int64             `json:"dauApprox"`        // distinct user_ids active in last 24h
	LastCalls          []LLMCallSnapshot `json:"lastCalls"`
	GeneratedAt        time.Time         `json:"generatedAt"`
}

// LLMCallSnapshot is the trimmed view of an llm_calls row used in the
// "recent activity" feed. Full row shape lives in observability/llmcalls.go;
// admin doesn't import observability to avoid a dependency loop, so we
// re-shape via a Mongo projection.
type LLMCallSnapshot struct {
	ID         string    `bson:"_id" json:"id"`
	UserID     string    `bson:"userId" json:"userId"`
	Provider   string    `bson:"provider" json:"provider"`
	Model      string    `bson:"model" json:"model"`
	Feature    string    `bson:"feature" json:"feature"`
	CostUSD    float64   `bson:"costUsd" json:"costUsd"`
	DurationMs int64     `bson:"durationMs" json:"durationMs"`
	Status     string    `bson:"status" json:"status"`
	CreatedAt  time.Time `bson:"createdAt" json:"createdAt"`
}

// OverviewRepository reads aggregates from llm_calls + users.
//
// The shape is narrow on purpose — Phase-1 dashboard only needs
// today's totals + last N calls + DAU. Per-feature / per-model
// breakdowns land alongside the dedicated /admin/costs page (P4).
type OverviewRepository interface {
	TodayMetrics(ctx context.Context, now time.Time) (spendUSD float64, callCount int64, err error)
	RecentLLMCalls(ctx context.Context, n int) ([]LLMCallSnapshot, error)
	ApproxDAU(ctx context.Context, since time.Time) (int64, error)
}

// OverviewMongoRepository implements OverviewRepository against the
// shared Mongo cluster. Reads only — never writes.
type OverviewMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewOverviewMongoRepository constructs the repo. No indexes to
// create — the queries here ride on the indexes already declared
// by observability (llm_calls.created_desc) and wardrobe.users.
func NewOverviewMongoRepository(client *mongo.Client, dbName string) *OverviewMongoRepository {
	return &OverviewMongoRepository{client: client, dbName: dbName}
}

func (r *OverviewMongoRepository) llmCallsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("llm_calls")
}

func (r *OverviewMongoRepository) usersCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("users")
}

// TodayMetrics aggregates today's llm_calls (UTC midnight to now).
// One Mongo $group pipeline returns both numbers — cheaper than two
// separate calls, and the result fits in a single $sum/$count pair.
func (r *OverviewMongoRepository) TodayMetrics(ctx context.Context, now time.Time) (float64, int64, error) {
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"createdAt": bson.M{"$gte": startOfDay}}}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"spend": bson.M{"$sum": "$costUsd"},
			"count": bson.M{"$sum": 1},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return 0, 0, err
	}
	defer cur.Close(ctx)

	var rows []struct {
		Spend float64 `bson:"spend"`
		Count int64   `bson:"count"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		// No calls today yet — legitimate zero state, not an error.
		return 0, 0, nil
	}
	return rows[0].Spend, rows[0].Count, nil
}

// RecentLLMCalls returns the last n calls regardless of user, sorted
// newest-first. Used by the dashboard's "recent activity" feed.
func (r *OverviewMongoRepository) RecentLLMCalls(ctx context.Context, n int) ([]LLMCallSnapshot, error) {
	if n <= 0 || n > 100 {
		n = 10
	}
	cur, err := r.llmCallsCol().Find(
		ctx,
		bson.M{},
		findOpts().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(n)),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []LLMCallSnapshot
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ApproxDAU returns the count of distinct user_ids whose users
// document was updated since `since`. Heuristic — replaced by a real
// activity-stream count when the events collection lands (P2-02).
func (r *OverviewMongoRepository) ApproxDAU(ctx context.Context, since time.Time) (int64, error) {
	count, err := r.usersCol().CountDocuments(ctx, bson.M{
		"updatedAt": bson.M{"$gte": since},
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
