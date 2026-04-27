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

// TracesRepository owns the read side of llm_calls. We deliberately
// keep this separate from the OverviewRepository — the queries
// shape differently (paginated vs aggregate) and the use sites
// don't share code worth deduping.
type TracesRepository interface {
	List(ctx context.Context, q TracesQuery) (TracesPage, error)
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

// List returns one page of llm_calls rows matching q. Sort is
// always createdAt-desc, _id-desc (tiebreaker so pagination is
// stable when many rows share the same millisecond).
func (r *TracesMongoRepository) List(ctx context.Context, q TracesQuery) (TracesPage, error) {
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}

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
