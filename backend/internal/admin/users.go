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

// UserOutfitBatch is one persisted outfit_jobs row, surfaced on the
// admin user-detail page's Outfits tab. Carries the candidates the
// LLM produced + the job's status + when it ran. Cross-domain read
// of the outfit_jobs collection (admin doesn't import the outfit
// package — same one-way pattern as wardrobe / moodboards).
type UserOutfitBatch struct {
	ID         string                   `bson:"_id"                json:"id"`
	UserID     string                   `bson:"userId"             json:"userId,omitempty"`
	Status     string                   `bson:"status"             json:"status"`
	Error      string                   `bson:"error,omitempty"    json:"error,omitempty"`
	CreatedAt  time.Time                `bson:"createdAt"          json:"createdAt"`
	UpdatedAt  time.Time                `bson:"updatedAt,omitempty" json:"updatedAt,omitempty"`
	Candidates []map[string]any         `bson:"outfits,omitempty"  json:"candidates,omitempty"`
}

// UserOutfitsPage is the response shape for /admin/v1/users/{id}/outfits.
type UserOutfitsPage struct {
	Batches    []UserOutfitBatch `json:"batches"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

// UserMoodboard mirrors moodboard.SavedMoodBoard for the admin
// user-detail page's Moodboards tab. The outfit payload is opaque
// (map[string]any) so the wire shape doesn't have to import the
// moodboard package's types — the FE renders a small subset
// (name, description, palette) and offers the rest as raw JSON for
// inspection.
type UserMoodboard struct {
	ID        string         `bson:"_id"                json:"id"`
	UserID    string         `bson:"userId"             json:"userId,omitempty"`
	Date      string         `bson:"date"               json:"date"`
	Outfit    map[string]any `bson:"outfit"             json:"outfit"`
	ImageURL  string         `bson:"imageUrl,omitempty" json:"imageUrl,omitempty"`
	CreatedAt time.Time      `bson:"createdAt"          json:"createdAt"`
}

// UserMoodboardsPage is the response shape for /admin/v1/users/{id}/moodboards.
type UserMoodboardsPage struct {
	Items      []UserMoodboard `json:"items"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

// UserSpendBucket is one (date, feature) row in the per-feature
// spend breakdown.
type UserSpendBucket struct {
	Date      string  `json:"date"`
	Feature   string  `json:"feature"`
	CostUSD   float64 `json:"costUsd"`
	CallCount int64   `json:"callCount"`
}

// UserCacheDailyBucket is one day's Anthropic prompt-cache totals
// for a user. Empty days are zero-filled at the backend so the FE
// sparkline renders without merge logic. (P4-03 / mootd-admin#31.)
type UserCacheDailyBucket struct {
	Date             string `json:"date"`
	CacheReadTokens  int64  `json:"cacheReadTokens"`
	CacheWriteTokens int64  `json:"cacheWriteTokens"`
	InputTokens      int64  `json:"inputTokens"`
	CallCount        int64  `json:"callCount,omitempty"`
}

// UserCacheDaily wraps the per-day buckets with rolled-up totals.
// Hit ratio is computed FE-side from the totals to keep the bucket
// shape stable across the per-user and global views.
type UserCacheDaily struct {
	Buckets          []UserCacheDailyBucket `json:"buckets"`
	TotalReadTokens  int64                  `json:"totalReadTokens"`
	TotalWriteTokens int64                  `json:"totalWriteTokens"`
	TotalInputTokens int64                  `json:"totalInputTokens"`
	ApproxSavingsUSD float64                `json:"approxSavingsUSD,omitempty"`
}

// UserSpendBreakdown is a 30-day per-feature stacked-spend payload.
// Pre-zero-filled at the backend so the FE chart can render
// directly without bucket-merging logic.
type UserSpendBreakdown struct {
	Buckets      []UserSpendBucket `json:"buckets"`
	TotalCostUSD float64           `json:"totalCostUsd"`
	Features     []string          `json:"features"`
	CacheDaily   *UserCacheDaily   `json:"cacheDaily,omitempty"`
}

// UserWardrobeItem is one clothing item surfaced on the admin
// user-detail page's Wardrobe tab. Mirrors the wardrobe package's
// ClothingItem with the same wire shape — we read the same Mongo
// docs.
type UserWardrobeItem struct {
	ID          string            `bson:"_id"          json:"id"`
	Category    string            `bson:"category"     json:"category"`
	Label       string            `bson:"label"        json:"label"`
	ImageURL    string            `bson:"imageUrl"     json:"imageUrl,omitempty"`
	PngImageURL string            `bson:"pngImageUrl"  json:"pngImageUrl,omitempty"`
	Traits      map[string]string `bson:"traits"       json:"traits,omitempty"`
	CreatedAt   time.Time         `bson:"createdAt"    json:"createdAt"`
}

// UserWardrobePage is the response shape for /admin/v1/users/{id}/wardrobe.
type UserWardrobePage struct {
	Items      []UserWardrobeItem `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

// UserDetail is the drill-through payload for the admin
// user-detail page (P1-06 / mootd-admin#11). Combines the existing
// scalar facts (UserSummary) with 30-day spend / call series and a
// page of recent calls. Future tabs (Wardrobe / Outfits /
// Moodboards / Budget) will be served via separate paginated
// endpoints; this stays additive.
type UserDetail struct {
	Summary         UserSummary       `json:"summary"`
	SpendSeries     []DailyMetric     `json:"spendSeries,omitempty"`
	CallCountSeries []DailyMetric     `json:"callCountSeries,omitempty"`
	RecentCalls     []LLMCallSnapshot `json:"recentCalls,omitempty"`
	TotalSpendUSD   float64           `json:"totalSpendUsd"`
	TotalCallCount  int64             `json:"totalCallCount"`
	GeneratedAt     time.Time         `json:"generatedAt"`

	// P2-03 (mootd-admin#20). Total time the user had the app
	// foregrounded, summed from `session_end.properties.durationMs`
	// over the trailing 7 days. Zero when no events recorded —
	// rendered as "—" on the FE in that case.
	Last7dSessionTimeMs int64 `json:"last7dSessionTimeMs,omitempty"`
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
	// FindDetail returns the full drill-through for a single user.
	// Returns (nil, nil) when the user doesn't exist (handler maps to 404).
	FindDetail(ctx context.Context, userID string) (*UserDetail, error)
	// SearchUsers powers the Cmd+K palette + global search bar.
	// Case-insensitive contains-match on email. Empty query returns []
	// (not an error) so caller debouncing is forgiving.
	SearchUsers(ctx context.Context, query string, maxHits int) ([]SearchHit, error)
	// ListWardrobe returns one page of a user's wardrobe items,
	// newest first, cursor-paginated on (createdAt desc, _id desc).
	ListWardrobe(ctx context.Context, userID, cursor string, limit int) ([]UserWardrobeItem, string, error)
	// ListMoodboards returns one page of a user's saved moodboards.
	// Same cursor flavour as ListWardrobe.
	ListMoodboards(ctx context.Context, userID, cursor string, limit int) ([]UserMoodboard, string, error)
	// SpendBreakdown returns 30-day per-feature spend for one user,
	// zero-filled and ordered by total-cost feature first.
	SpendBreakdown(ctx context.Context, userID string, now time.Time) (*UserSpendBreakdown, error)
	// ListOutfitBatches returns one page of outfit_jobs rows for a
	// user — each row is one generation request with its 3-4
	// candidate outfits. Cursor pagination on (createdAt desc,
	// _id desc) — same flavour as ListWardrobe + ListMoodboards.
	ListOutfitBatches(ctx context.Context, userID, cursor string, limit int) ([]UserOutfitBatch, string, error)
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

// FindDetail builds the drill-through payload for one user. Stitches:
//   - The existing UserSummary (item counts + 7d aggregates)
//   - 30-day daily spend + call-count series for spark rendering
//   - Last 25 LLM calls for the activity feed
//   - Lifetime totals (totalSpendUsd, totalCallCount)
//
// Three Mongo round-trips: the user doc itself, one $group for series,
// one Find+Limit for recent calls. ApproxDAU-style index access; no
// scans even for high-volume users.
//
// Returns (nil, nil) when the user doesn't exist so the handler can
// emit 404 without inspecting an error type.
func (r *UsersMongoRepository) FindDetail(ctx context.Context, userID string) (*UserDetail, error) {
	if userID == "" {
		return nil, nil
	}

	// 1) The user doc.
	var doc userListDoc
	err := r.usersCol().FindOne(ctx, bson.M{"_id": userID}).Decode(&doc)
	if err != nil {
		if err.Error() == "mongo: no documents in result" {
			return nil, nil
		}
		// errors.Is is the proper check but we avoid pulling
		// mongo.ErrNoDocuments into this file's surface.
		return nil, err
	}

	// 2) The existing summary fan-out: counts + 7d aggregates +
	// spend join. populateSummaries handles all of this.
	summaries := r.populateSummaries(ctx, []userListDoc{doc})
	var summary UserSummary
	if len(summaries) > 0 {
		summary = summaries[0]
	} else {
		summary = UserSummary{ID: doc.ID, Email: doc.Email, Name: doc.Name, SignupDate: doc.CreatedAt, LastActiveAt: doc.UpdatedAt}
	}

	// 3) 30-day daily series for this user (spend + call count).
	startOfWindow := time.Now().UTC().Truncate(24 * time.Hour).Add(-29 * 24 * time.Hour)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"userId":    userID,
			"createdAt": bson.M{"$gte": startOfWindow},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$createdAt", "timezone": "UTC"}},
			"spend": bson.M{"$sum": "$costUsd"},
			"count": bson.M{"$sum": 1},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		// Best-effort: failed series shouldn't fail the whole detail.
		// Log path should be in the handler.
		cur = nil
	}
	type bucket struct {
		ID    string  `bson:"_id"`
		Spend float64 `bson:"spend"`
		Count int64   `bson:"count"`
	}
	var buckets []bucket
	if cur != nil {
		_ = cur.All(ctx, &buckets)
		_ = cur.Close(ctx)
	}
	byDate := make(map[string]bucket, len(buckets))
	for _, b := range buckets {
		byDate[b.ID] = b
	}
	spendSeries := make([]DailyMetric, 0, 30)
	countSeries := make([]DailyMetric, 0, 30)
	for i := 0; i < 30; i++ {
		d := startOfWindow.Add(time.Duration(i) * 24 * time.Hour)
		key := d.Format("2006-01-02")
		b := byDate[key]
		spendSeries = append(spendSeries, DailyMetric{Date: key, Value: b.Spend})
		countSeries = append(countSeries, DailyMetric{Date: key, Value: float64(b.Count)})
	}

	// 4) Last 25 calls for this user.
	callsCur, err := r.llmCallsCol().Find(
		ctx,
		bson.M{"userId": userID},
		findOpts().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).SetLimit(25),
	)
	var calls []LLMCallSnapshot
	if err == nil && callsCur != nil {
		_ = callsCur.All(ctx, &calls)
		_ = callsCur.Close(ctx)
	}

	// 5) Lifetime totals — same aggregation, no date bound.
	var totalSpend float64
	var totalCount int64
	totalsCur, err := r.llmCallsCol().Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"userId": userID}}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"spend": bson.M{"$sum": "$costUsd"},
			"count": bson.M{"$sum": 1},
		}}},
	})
	if err == nil && totalsCur != nil {
		var rows []struct {
			Spend float64 `bson:"spend"`
			Count int64   `bson:"count"`
		}
		_ = totalsCur.All(ctx, &rows)
		_ = totalsCur.Close(ctx)
		if len(rows) > 0 {
			totalSpend = rows[0].Spend
			totalCount = rows[0].Count
		}
	}

	// 5) Last-7d session time (P2-03 / mootd-admin#20). Aggregated
	// from session_end events. Best-effort; zero on failure.
	sessionMs := r.last7dSessionTimeMs(ctx, userID)

	return &UserDetail{
		Summary:             summary,
		SpendSeries:         spendSeries,
		CallCountSeries:     countSeries,
		RecentCalls:         calls,
		TotalSpendUSD:       totalSpend,
		TotalCallCount:      totalCount,
		Last7dSessionTimeMs: sessionMs,
		GeneratedAt:         time.Now().UTC(),
	}, nil
}

// last7dSessionTimeMs sums durationMs across the user's
// session_end events in the trailing 7 days. Read-side of the
// SDK pipeline (P2-01 → P2-02 → here). Best-effort: returns 0
// on any error since session time is a "nice to have" panel,
// not the source of truth.
func (r *UsersMongoRepository) last7dSessionTimeMs(ctx context.Context, userID string) int64 {
	startOfWindow := time.Now().UTC().Add(-7 * 24 * time.Hour)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"userId":    userID,
			"name":      "session_end",
			"createdAt": bson.M{"$gte": startOfWindow},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"total": bson.M{"$sum": "$properties.durationMs"},
		}}},
	}
	cur, err := r.eventsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return 0
	}
	defer cur.Close(ctx)
	var rows []struct {
		Total int64 `bson:"total"`
	}
	if err := cur.All(ctx, &rows); err != nil || len(rows) == 0 {
		return 0
	}
	return rows[0].Total
}

// eventsCol returns the events collection. Cross-domain read
// (the events package writes; admin reads), same one-way
// pattern we use for outfit_jobs / moodboards / wardrobe_items.
func (r *UsersMongoRepository) eventsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("events")
}

// ListWardrobe returns one page of a user's wardrobe items.
// Cursor encodes the previous page's last _id; sort is
// (createdAt desc, _id desc) for stable pagination across same-
// millisecond rows.
//
// We deliberately re-read the wardrobe_items collection directly
// rather than going through the wardrobe package's Repository.
// The admin scope reads only the shape this endpoint returns —
// re-using wardrobe.Repository would tangle the import graph
// (admin → wardrobe → … and there's no shared interface today).
// The doc shape is stable; if it ever drifts, this is a one-line
// edit alongside the wardrobe package's own ClothingItem.
func (r *UsersMongoRepository) ListWardrobe(ctx context.Context, userID, cursor string, limit int) ([]UserWardrobeItem, string, error) {
	if userID == "" {
		return nil, "", errors.New("admin: userID required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	filter := bson.M{"userId": userID}
	if cursor != "" {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	cur, err := r.wardrobeCol().Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit+1))) // +1 to detect more
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var items []UserWardrobeItem
	if err := cur.All(ctx, &items); err != nil {
		return nil, "", err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}
	return items, nextCursor, nil
}

// ListMoodboards returns one page of saved moodboards for a user.
// Cursor pagination on (createdAt desc, _id desc) — same flavour as
// ListWardrobe so the FE infinite-scroll hook is reusable.
func (r *UsersMongoRepository) ListMoodboards(ctx context.Context, userID, cursor string, limit int) ([]UserMoodboard, string, error) {
	if userID == "" {
		return nil, "", errors.New("admin: userID required")
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	filter := bson.M{"userId": userID}
	if cursor != "" {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	cur, err := r.moodboardsCol().Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit+1))) // +1 to detect more
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var items []UserMoodboard
	if err := cur.All(ctx, &items); err != nil {
		return nil, "", err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}
	return items, nextCursor, nil
}

// SpendBreakdown returns the user's last-30-day per-feature spend.
//
// Single $group aggregation keyed on (feature, day-bucket); the
// rest is in-memory zero-fill so the chart renders 30 entries per
// feature with no gaps.
func (r *UsersMongoRepository) SpendBreakdown(ctx context.Context, userID string, now time.Time) (*UserSpendBreakdown, error) {
	if userID == "" {
		return nil, errors.New("admin: userID required")
	}
	startOfWindow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-29 * 24 * time.Hour)

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"userId":    userID,
			"createdAt": bson.M{"$gte": startOfWindow},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"feature": "$feature",
				"day":     bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$createdAt", "timezone": "UTC"}},
			},
			"costUsd":   bson.M{"$sum": "$costUsd"},
			"callCount": bson.M{"$sum": 1},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	type bucket struct {
		ID struct {
			Feature string `bson:"feature"`
			Day     string `bson:"day"`
		} `bson:"_id"`
		CostUSD   float64 `bson:"costUsd"`
		CallCount int64   `bson:"callCount"`
	}
	var rows []bucket
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}

	// Index actual data by (feature, day) and track distinct features
	// + per-feature totals for the ordering below.
	byKey := make(map[string]bucket, len(rows))
	totalsByFeature := map[string]float64{}
	for _, b := range rows {
		key := b.ID.Feature + "|" + b.ID.Day
		byKey[key] = b
		totalsByFeature[b.ID.Feature] += b.CostUSD
	}

	// Order features by total spend desc — gives the largest
	// stacked-area band the most prominent layer in the chart.
	features := make([]string, 0, len(totalsByFeature))
	for f := range totalsByFeature {
		features = append(features, f)
	}
	sort.SliceStable(features, func(i, j int) bool {
		return totalsByFeature[features[i]] > totalsByFeature[features[j]]
	})

	// Zero-fill: emit a bucket for every (feature, day) pair across
	// the 30-day window so the FE chart axes stay uniform without
	// having to compute missing-day inserts on its side.
	buckets := make([]UserSpendBucket, 0, len(features)*30)
	var totalCost float64
	for _, f := range features {
		for i := 0; i < 30; i++ {
			d := startOfWindow.Add(time.Duration(i) * 24 * time.Hour)
			day := d.Format("2006-01-02")
			b := byKey[f+"|"+day]
			buckets = append(buckets, UserSpendBucket{
				Date:      day,
				Feature:   f,
				CostUSD:   b.CostUSD,
				CallCount: b.CallCount,
			})
			totalCost += b.CostUSD
		}
	}

	// Cache trend (P4-03 / mootd-admin#31). Best-effort — if the
	// cache aggregation errors we still return spend; the FE
	// renders the cache panel as "no data" when the field is nil.
	cacheDaily, cerr := r.cacheBreakdown(ctx, userID, startOfWindow)
	if cerr != nil {
		// Don't bubble: spend is the primary product of this
		// endpoint, cache is an additional analytic surface.
		// A query failure here would silently hide the panel,
		// which beats failing the whole spend tab.
		cacheDaily = nil
	}

	return &UserSpendBreakdown{
		Buckets:      buckets,
		TotalCostUSD: totalCost,
		Features:     features,
		CacheDaily:   cacheDaily,
	}, nil
}

// cacheBreakdown aggregates per-day Anthropic prompt-cache usage
// for a user across the same 30-day window the spend chart uses
// (P4-03 / mootd-admin#31). Single $group keyed on day-bucket;
// rest is in-memory zero-fill.
//
// Filtered to provider=anthropic since cache reads/writes are
// Anthropic-specific. OpenAI and Ollama callsalso store
// cacheReadTokens=0 / cacheWriteTokens=0, so leaving them in the
// match would skew the ratios.
//
// Approx savings uses the same Sonnet-4.5 list price as the global
// /overview endpoint ($3/Mtok input - $0.30/Mtok cached). A future
// refinement can join model_prices for exact pricing per row.
func (r *UsersMongoRepository) cacheBreakdown(ctx context.Context, userID string, startOfWindow time.Time) (*UserCacheDaily, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"userId":    userID,
			"provider":  "anthropic",
			"createdAt": bson.M{"$gte": startOfWindow},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":         bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$createdAt", "timezone": "UTC"}},
			"readTokens":  bson.M{"$sum": "$cacheReadTokens"},
			"writeTokens": bson.M{"$sum": "$cacheWriteTokens"},
			"inputTokens": bson.M{"$sum": "$inputTokens"},
			"callCount":   bson.M{"$sum": 1},
		}}},
	}
	cur, err := r.llmCallsCol().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	type cacheRow struct {
		Day         string `bson:"_id"`
		ReadTokens  int64  `bson:"readTokens"`
		WriteTokens int64  `bson:"writeTokens"`
		InputTokens int64  `bson:"inputTokens"`
		CallCount   int64  `bson:"callCount"`
	}
	var rows []cacheRow
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}

	// Skip the panel entirely if the user has no Anthropic calls in
	// the window — avoids rendering an empty "0% hit ratio" chart
	// that's actually "no data."
	if len(rows) == 0 {
		return nil, nil
	}

	byDay := make(map[string]cacheRow, len(rows))
	var totalRead, totalWrite, totalInput int64
	for _, r := range rows {
		byDay[r.Day] = r
		totalRead += r.ReadTokens
		totalWrite += r.WriteTokens
		totalInput += r.InputTokens
	}

	buckets := make([]UserCacheDailyBucket, 0, 30)
	for i := 0; i < 30; i++ {
		d := startOfWindow.Add(time.Duration(i) * 24 * time.Hour)
		day := d.Format("2006-01-02")
		b := byDay[day]
		buckets = append(buckets, UserCacheDailyBucket{
			Date:             day,
			CacheReadTokens:  b.ReadTokens,
			CacheWriteTokens: b.WriteTokens,
			InputTokens:      b.InputTokens,
			CallCount:        b.CallCount,
		})
	}

	// Approx savings: cacheRead × ($3/M list - $0.30/M cached).
	const fullInputPerToken = 3.0 / 1_000_000
	const cachedReadPerToken = 0.30 / 1_000_000
	savings := float64(totalRead) * (fullInputPerToken - cachedReadPerToken)

	return &UserCacheDaily{
		Buckets:          buckets,
		TotalReadTokens:  totalRead,
		TotalWriteTokens: totalWrite,
		TotalInputTokens: totalInput,
		ApproxSavingsUSD: savings,
	}, nil
}

// outfitJobsCol returns the outfit_jobs collection. Same cross-domain
// read pattern as moodboardsCol/wardrobeCol — the admin reads what
// the outfit package writes, without importing it.
func (r *UsersMongoRepository) outfitJobsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("outfit_jobs")
}

// ListOutfitBatches returns one page of outfit_jobs rows for a user.
// Each row carries the candidates the LLM proposed, plus the job's
// terminal status. Cursor pagination on (createdAt desc, _id desc).
//
// We deliberately accept all statuses (pending / processing /
// completed / failed) so admins can see in-flight generations and
// debug failures alongside successes. Failed rows have status =
// "failed" + a populated error string.
func (r *UsersMongoRepository) ListOutfitBatches(ctx context.Context, userID, cursor string, limit int) ([]UserOutfitBatch, string, error) {
	if userID == "" {
		return nil, "", errors.New("admin: userID required")
	}
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	filter := bson.M{"userId": userID}
	if cursor != "" {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	cur, err := r.outfitJobsCol().Find(ctx, filter,
		options.Find().
			SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit+1))) // +1 to detect more
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var batches []UserOutfitBatch
	if err := cur.All(ctx, &batches); err != nil {
		return nil, "", err
	}
	hasMore := len(batches) > limit
	if hasMore {
		batches = batches[:limit]
	}
	nextCursor := ""
	if hasMore && len(batches) > 0 {
		nextCursor = batches[len(batches)-1].ID
	}
	return batches, nextCursor, nil
}

// errUnsupportedSort returned when the caller passes a sort key we
// haven't mapped. Public so handlers can format the response uniformly.
var errUnsupportedSort = errors.New("admin: unsupported sort key")
