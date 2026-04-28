package admin

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// UserSummary is the row shape returned by GET /admin/v1/users.
//
// Email is denormalised here AS-STORED (cleartext); the handler
// applies redaction before serialising when the caller lacks the
// users:pii permission. Phase 0 has every admin role at full PII
// scope (P5-01 narrows it).
//
// Last7dSpendUsd is the sum of llm_calls.costUsd in the trailing 7
// days (server time, UTC). Computed in a single $group aggregation
// per page, not per-row, to keep the per-page cost flat.
type UserSummary struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Name           string    `json:"name,omitempty"`
	SignupDate     time.Time `json:"signupDate"`
	LastActiveAt   time.Time `json:"lastActiveAt,omitempty"`
	WardrobeCount  int64     `json:"wardrobeCount"`
	OutfitCount    int64     `json:"outfitCount"`
	MoodboardCount int64     `json:"moodboardCount"`
	Last7dUploads  int64     `json:"last7dUploads"`
	Last7dOutfits  int64     `json:"last7dOutfits"`
	Last7dSpendUsd float64   `json:"last7dSpendUsd"`
	IsOverBudget   bool      `json:"isOverBudget"` // false until P4-01 lands
	Tier           string    `json:"tier,omitempty"`
}

// UsersListResponse is the wrapper around the page of summaries.
type UsersListResponse struct {
	Users      []UserSummary `json:"users"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// UsersQuery is the filter set accepted by GET /admin/v1/users. Kept
// as a struct so the handler doesn't have a 6-arg call site.
type UsersQuery struct {
	Search string // contains-match on email
	Tier   string // free | paid | founder | beta — empty = no filter
	Active bool   // true → only active in last 30 days
	Sort   string // "-signup" | "-last_active" | "-uploads" | "-outfits" | "-spend_7d"
	Cursor string // pagination cursor (createdAt _id for indexed sort, offset for computed sort)
	Limit  int    // 1..100; default 20
}

// UsersRepository handles cross-collection reads needed by the admin
// users list. It reads the existing user/wardrobe/outfit/moodboard
// collections + llm_calls — never writes them.
type UsersRepository interface {
	ListSummaries(ctx context.Context, q UsersQuery) ([]UserSummary, string, error)
}

// UsersMongoRepository is the production implementation.
//
// Two pagination flavours, dispatched on the sort key:
//
//   - Indexed sort (`-signup`, `-last_active`): cursor pagination on
//     the user document's _id, ordered by an indexed field. O(limit)
//     per page.
//   - Computed sort (`-uploads`, `-outfits`, `-spend_7d`): the sort
//     key isn't on the user document, so we fetch a generous superset
//     (capped at maxUsersScan), compute summaries, sort in Go, then
//     paginate by offset. Reasonable until the user count nears the
//     cap; we'll move to a dedicated rollup collection (#23) when it
//     does.
type UsersMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewUsersMongoRepository constructs a UsersMongoRepository.
func NewUsersMongoRepository(client *mongo.Client, dbName string) *UsersMongoRepository {
	return &UsersMongoRepository{client: client, dbName: dbName}
}

// maxUsersScan caps the in-memory sort path. At Phase 0 / 1 we have
// well under this; tighten or move to a rollup once the cap binds.
const maxUsersScan = 500

func (r *UsersMongoRepository) usersCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("users")
}
func (r *UsersMongoRepository) wardrobeCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("wardrobe_items")
}
func (r *UsersMongoRepository) outfitsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("outfits")
}
func (r *UsersMongoRepository) moodboardsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("moodboards")
}
func (r *UsersMongoRepository) llmCallsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("llm_calls")
}

// userListDoc is just the subset of the user document we need for
// the summary. Avoids a hand-defined struct shared with auth/user
// packages — admin reads, never writes the user shape.
type userListDoc struct {
	ID        string    `bson:"_id"`
	Email     string    `bson:"email"`
	Name      string    `bson:"name"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

func isComputedSort(s string) bool {
	switch s {
	case "-uploads", "-outfits", "-spend_7d":
		return true
	}
	return false
}

// ListSummaries returns one page of user summaries plus the cursor for
// the next page. Dispatches on whether the sort key is indexed (cheap
// cursor flow) or computed (in-memory sort flow).
func (r *UsersMongoRepository) ListSummaries(ctx context.Context, q UsersQuery) ([]UserSummary, string, error) {
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	filter := bson.M{}
	if q.Search != "" {
		// Case-insensitive contains. Acceptable performance at our
		// scale; if list pages grow large we'd add a text index.
		filter["email"] = bson.M{"$regex": q.Search, "$options": "i"}
	}
	if q.Active {
		// "Active" = updated in the last 30 days. The user collection
		// doesn't carry a lastActiveAt today; we approximate from
		// updatedAt. P2-03 (session tracking) will replace this with
		// a real activity stream.
		filter["updatedAt"] = bson.M{"$gt": time.Now().UTC().Add(-30 * 24 * time.Hour)}
	}

	if isComputedSort(q.Sort) {
		return r.listComputedSort(ctx, q, filter, limit)
	}
	return r.listIndexedSort(ctx, q, filter, limit)
}

// listIndexedSort handles `-signup` (default) and `-last_active`.
// Cursor encodes the previous page's last user _id; the matching
// indexed field provides the order.
func (r *UsersMongoRepository) listIndexedSort(
	ctx context.Context, q UsersQuery, filter bson.M, limit int,
) ([]UserSummary, string, error) {
	if q.Cursor != "" {
		// Cursor is the previous page's last user _id. Pull rows
		// older than that one (sort is already createdAt-desc, so
		// "_id < cursor" lines up with the natural order).
		filter["_id"] = bson.M{"$lt": q.Cursor}
	}

	sortSpec := bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}
	if q.Sort == "-last_active" {
		sortSpec = bson.D{{Key: "updatedAt", Value: -1}, {Key: "_id", Value: -1}}
	}

	cur, err := r.usersCol().Find(ctx, filter,
		options.Find().SetSort(sortSpec).SetLimit(int64(limit+1))) // +1 to know if there's another page
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []userListDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	hasMore := len(docs) > limit
	if hasMore {
		docs = docs[:limit]
	}

	summaries := r.populateSummaries(ctx, docs)

	nextCursor := ""
	if hasMore && len(summaries) > 0 {
		nextCursor = summaries[len(summaries)-1].ID
	}
	return summaries, nextCursor, nil
}

// listComputedSort fetches up to maxUsersScan users matching the filter,
// computes their summaries, sorts in Go, and paginates by offset. The
// cursor is the next offset as a stringified int — opaque to the
// frontend, parsed here.
func (r *UsersMongoRepository) listComputedSort(
	ctx context.Context, q UsersQuery, filter bson.M, limit int,
) ([]UserSummary, string, error) {
	offset := 0
	if q.Cursor != "" {
		if n, err := strconv.Atoi(q.Cursor); err == nil && n >= 0 {
			offset = n
		}
	}

	cur, err := r.usersCol().Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(maxUsersScan)))
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var docs []userListDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", err
	}

	all := r.populateSummaries(ctx, docs)

	sortSummaries(all, q.Sort)

	if offset >= len(all) {
		return []UserSummary{}, "", nil
	}
	end := offset + limit
	hasMore := false
	if end >= len(all) {
		end = len(all)
	} else {
		hasMore = true
	}
	page := all[offset:end]
	nextCursor := ""
	if hasMore {
		nextCursor = strconv.Itoa(end)
	}
	return page, nextCursor, nil
}

// populateSummaries fans out the per-user count queries + a single
// llm_calls $group aggregation for the spend column. Per-user counts
// are tiny indexed point queries; at 20 users/page that's 80 ops per
// request — comfortably under 100ms total against local Mongo.
func (r *UsersMongoRepository) populateSummaries(ctx context.Context, docs []userListDoc) []UserSummary {
	weekAgo := time.Now().UTC().Add(-7 * 24 * time.Hour)

	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	// One $group call covers spend for the entire page. Best-effort —
	// a failure here just leaves spend at 0; we never want a single
	// aggregation hiccup to fail the whole users list.
	spendMap, _ := r.spend7dByUser(ctx, ids, weekAgo)

	summaries := make([]UserSummary, 0, len(docs))
	for _, d := range docs {
		s := UserSummary{
			ID:             d.ID,
			Email:          d.Email,
			Name:           d.Name,
			SignupDate:     d.CreatedAt,
			LastActiveAt:   d.UpdatedAt,
			Last7dSpendUsd: spendMap[d.ID],
		}
		s.WardrobeCount, _ = r.wardrobeCol().CountDocuments(ctx, bson.M{"userId": d.ID})
		s.OutfitCount, _ = r.outfitsCol().CountDocuments(ctx, bson.M{"userId": d.ID})
		s.MoodboardCount, _ = r.moodboardsCol().CountDocuments(ctx, bson.M{"userId": d.ID})
		s.Last7dUploads, _ = r.wardrobeCol().CountDocuments(ctx, bson.M{
			"userId":    d.ID,
			"createdAt": bson.M{"$gt": weekAgo},
		})
		s.Last7dOutfits, _ = r.outfitsCol().CountDocuments(ctx, bson.M{
			"userId":    d.ID,
			"createdAt": bson.M{"$gt": weekAgo},
		})
		summaries = append(summaries, s)
	}
	return summaries
}

// spend7dByUser sums llm_calls.costUsd grouped by userId for the
// supplied page of ids, restricted to [weekAgo, now). One aggregation
// per page; relies on the (userId_1_createdAt_-1) index on llm_calls.
func (r *UsersMongoRepository) spend7dByUser(
	ctx context.Context, ids []string, since time.Time,
) (map[string]float64, error) {
	if len(ids) == 0 {
		return map[string]float64{}, nil
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"userId":    bson.M{"$in": ids},
			"createdAt": bson.M{"$gte": since},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$userId",
			"spend": bson.M{"$sum": "$costUsd"},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var rows []struct {
		ID    string  `bson:"_id"`
		Spend float64 `bson:"spend"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(rows))
	for _, row := range rows {
		out[row.ID] = row.Spend
	}
	return out, nil
}

// sortSummaries orders an in-memory slice by the requested key. Stable
// to keep ties in createdAt-desc order (the underlying scan order).
func sortSummaries(s []UserSummary, key string) {
	switch key {
	case "-uploads":
		sort.SliceStable(s, func(i, j int) bool { return s[i].Last7dUploads > s[j].Last7dUploads })
	case "-outfits":
		sort.SliceStable(s, func(i, j int) bool { return s[i].Last7dOutfits > s[j].Last7dOutfits })
	case "-spend_7d":
		sort.SliceStable(s, func(i, j int) bool { return s[i].Last7dSpendUsd > s[j].Last7dSpendUsd })
	}
}

// errUnsupportedSort returned when the caller passes a sort key we
// haven't mapped. Public so handlers can format the response uniformly.
var errUnsupportedSort = errors.New("admin: unsupported sort key")
