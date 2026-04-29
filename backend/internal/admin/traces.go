package admin

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// TracesQuery is the filter set accepted by GET /admin/v1/traces.
// All fields optional — the empty query returns the most recent
// page across all users / models / features.
type TracesQuery struct {
	UserID     string     // exact match
	Model      string     // exact match
	Feature    string     // exact match (e.g. "outfit_generate")
	Status     string     // "success" | "error" | "timeout"
	MinCostUSD float64    // >= filter; 0 disables the floor
	From       *time.Time // createdAt >= From
	To         *time.Time // createdAt < To (exclusive — half-open interval)
	Cursor     string     // pagination cursor (last row's _id from previous page)
	Limit      int        // 1..100; default 25
}

// TracesPage is the wire shape returned to the admin UI.
type TracesPage struct {
	Calls      []LLMCallSnapshot `json:"calls"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

// LLMCallDetail is the full llm_calls row including P1-11 archival
// fields (systemPrompt / userMessage / responseRaw / wardrobeItemIds).
// Returned by GET /admin/v1/traces/{id} for the prompt-viewer side
// panel (P1-12 / mootd-admin#17). Excluded from the list response
// by design — list rows ship the trim LLMCallSnapshot, the detail
// page fetches the heavy fields on demand.
type LLMCallDetail struct {
	ID               string    `bson:"_id" json:"id"`
	UserID           string    `bson:"userId" json:"userId"`
	UserEmail        string    `bson:"-" json:"userEmail,omitempty"` // resolved server-side
	Provider         string    `bson:"provider" json:"provider"`
	Model            string    `bson:"model" json:"model"`
	Feature          string    `bson:"feature" json:"feature"`
	CostUSD          float64   `bson:"costUsd" json:"costUsd"`
	InputTokens      int64     `bson:"inputTokens" json:"inputTokens,omitempty"`
	OutputTokens     int64     `bson:"outputTokens" json:"outputTokens,omitempty"`
	CacheReadTokens  int64     `bson:"cacheReadTokens" json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64     `bson:"cacheWriteTokens" json:"cacheWriteTokens,omitempty"`
	DurationMs       int64     `bson:"durationMs" json:"durationMs"`
	Status           string    `bson:"status" json:"status"`
	ErrorMsg         string    `bson:"errorMsg,omitempty" json:"errorMsg,omitempty"`
	PromptVersion    string    `bson:"promptVersion,omitempty" json:"promptVersion,omitempty"`
	PromptHash       string    `bson:"promptHash,omitempty" json:"promptHash,omitempty"`
	SystemPrompt     string    `bson:"systemPrompt,omitempty" json:"systemPrompt,omitempty"`
	UserMessage      string    `bson:"userMessage,omitempty" json:"userMessage,omitempty"`
	ResponseRaw      string    `bson:"responseRaw,omitempty" json:"responseRaw,omitempty"`
	WardrobeItemIDs  []string  `bson:"wardrobeItemIds,omitempty" json:"wardrobeItemIds,omitempty"`
	CreatedAt        time.Time `bson:"createdAt" json:"createdAt"`
}

// TracesSummary is the aggregate over the same filter set as List.
// Independent of pagination; powers the "1,234 calls · $45.20 ·
// avg 4.2s · p95 12.1s" strip on the /traces page.
type TracesSummary struct {
	TotalCount     int64   `json:"totalCount"`
	TotalCostUSD   float64 `json:"totalCostUsd"`
	AvgDurationMs  int64   `json:"avgDurationMs"`
	P95DurationMs  int64   `json:"p95DurationMs"`
}

// TracesRepository owns the read side of llm_calls. We deliberately
// keep this separate from the OverviewRepository — the queries
// shape differently (paginated vs aggregate) and the use sites
// don't share code worth deduping.
type TracesRepository interface {
	List(ctx context.Context, q TracesQuery) (TracesPage, error)
	// Summary aggregates over the same filter — count, spend, mean
	// + p95 latency. Pagination params on q are ignored.
	Summary(ctx context.Context, q TracesQuery) (TracesSummary, error)
	// IterAll streams every row matching the filter (no pagination).
	// Used by CSV export; capped at maxRows by the caller.
	IterAll(ctx context.Context, q TracesQuery, maxRows int) ([]LLMCallSnapshot, error)
	// FindDetail returns the full llm_calls row by id, including the
	// archival fields (systemPrompt / userMessage / responseRaw /
	// wardrobeItemIds). Returns (nil, nil) when not found so the
	// handler can map to 404 without inspecting an error type.
	FindDetail(ctx context.Context, id string) (*LLMCallDetail, error)
}

// TracesMongoRepository implements TracesRepository against the
// shared cluster.
type TracesMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewTracesMongoRepository constructs the repo. No new indexes —
// the queries below ride on the indexes declared by the
// observability package (llm_calls_user_created, _model_created,
// _feature_created, _created_desc).
func NewTracesMongoRepository(client *mongo.Client, dbName string) *TracesMongoRepository {
	return &TracesMongoRepository{client: client, dbName: dbName}
}

func (r *TracesMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("llm_calls")
}

// buildFilter translates a TracesQuery into the bson filter used
// by all read paths. Cursor handling is intentionally NOT included
// here — it's specific to the paginated List path and stitched on
// after this returns.
func buildTracesFilter(q TracesQuery) bson.M {
	filter := bson.M{}
	if q.UserID != "" {
		filter["userId"] = q.UserID
	}
	if q.Model != "" {
		filter["model"] = q.Model
	}
	if q.Feature != "" {
		filter["feature"] = q.Feature
	}
	if q.Status != "" {
		filter["status"] = q.Status
	}
	if q.MinCostUSD > 0 {
		filter["costUsd"] = bson.M{"$gte": q.MinCostUSD}
	}
	if q.From != nil || q.To != nil {
		ts := bson.M{}
		if q.From != nil {
			ts["$gte"] = q.From.UTC()
		}
		if q.To != nil {
			ts["$lt"] = q.To.UTC()
		}
		filter["createdAt"] = ts
	}
	return filter
}

// List returns one page of llm_calls rows matching q. Sort is
// always createdAt-desc, _id-desc (tiebreaker so pagination is
// stable when many rows share the same millisecond).
func (r *TracesMongoRepository) List(ctx context.Context, q TracesQuery) (TracesPage, error) {
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	filter := buildTracesFilter(q)
	if q.Cursor != "" {
		// Cursor encodes the last row's _id — pull rows whose _id
		// is "less than" it under the descending sort.
		filter["_id"] = bson.M{"$lt": q.Cursor}
	}

	cur, err := r.col().Find(
		ctx, filter,
		findOpts().
			SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit+1)), // +1 to know whether there's another page
	)
	if err != nil {
		return TracesPage{}, err
	}
	defer cur.Close(ctx)

	var rows []LLMCallSnapshot
	if err := cur.All(ctx, &rows); err != nil {
		return TracesPage{}, err
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	page := TracesPage{Calls: rows}
	if hasMore && len(rows) > 0 {
		page.NextCursor = rows[len(rows)-1].ID
	}
	return page, nil
}

// Summary computes aggregate count, spend, mean and p95 latency over
// the filter. One $group pipeline using $percentile (Mongo 7.0+).
// Approximate percentile method — exact would scan and sort the
// whole match; approximate uses a t-digest internally and is fine
// for the "rough p95" the strip displays.
func (r *TracesMongoRepository) Summary(ctx context.Context, q TracesQuery) (TracesSummary, error) {
	filter := buildTracesFilter(q)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$group", Value: bson.M{
			"_id":           nil,
			"totalCount":    bson.M{"$sum": 1},
			"totalCostUsd":  bson.M{"$sum": "$costUsd"},
			"avgDurationMs": bson.M{"$avg": "$durationMs"},
			"p95":           bson.M{"$percentile": bson.M{"input": "$durationMs", "p": []float64{0.95}, "method": "approximate"}},
		}}},
	}
	cur, err := r.col().Aggregate(ctx, pipeline)
	if err != nil {
		return TracesSummary{}, err
	}
	defer cur.Close(ctx)

	var rows []struct {
		TotalCount    int64     `bson:"totalCount"`
		TotalCostUSD  float64   `bson:"totalCostUsd"`
		AvgDurationMs float64   `bson:"avgDurationMs"`
		P95           []float64 `bson:"p95"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return TracesSummary{}, err
	}
	if len(rows) == 0 {
		return TracesSummary{}, nil
	}
	row := rows[0]
	p95 := int64(0)
	if len(row.P95) > 0 {
		p95 = int64(row.P95[0])
	}
	return TracesSummary{
		TotalCount:    row.TotalCount,
		TotalCostUSD:  row.TotalCostUSD,
		AvgDurationMs: int64(row.AvgDurationMs),
		P95DurationMs: p95,
	}, nil
}

// IterAll returns every row matching the filter, capped at maxRows.
// Used by CSV export; the cap protects the server from a no-filter
// export blowing up memory. Caller's responsibility to communicate
// truncation if len(result) == maxRows.
func (r *TracesMongoRepository) IterAll(ctx context.Context, q TracesQuery, maxRows int) ([]LLMCallSnapshot, error) {
	if maxRows <= 0 {
		maxRows = 50_000
	}
	filter := buildTracesFilter(q)
	cur, err := r.col().Find(
		ctx, filter,
		findOpts().
			SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(maxRows)),
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

// FindDetail returns the full llm_calls row by id, including the
// P1-11 archival fields. Returns (nil, nil) when not found so the
// handler can emit 404 without inspecting an error type.
func (r *TracesMongoRepository) FindDetail(ctx context.Context, id string) (*LLMCallDetail, error) {
	if id == "" {
		return nil, nil
	}
	var doc LLMCallDetail
	err := r.col().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// errInvalidTracesQuery is returned by the handler's parser when a
// query parameter is malformed (e.g. minCost not a float).
var errInvalidTracesQuery = errors.New("admin: invalid traces query")

// parseFloat0 is a tolerant float parser — empty + invalid both
// return 0 (the "no filter" sentinel). Used for minCost where 0
// already means "no minimum."
func parseFloat0(s string) float64 {
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return 0
}

// parseTimePtr parses an RFC-3339 timestamp; returns nil on empty
// or invalid (handler treats both as "no bound").
func parseTimePtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	return nil
}
