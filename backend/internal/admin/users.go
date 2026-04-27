package admin

import (
	"context"
	"errors"
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
// Cost-related fields (last7dSpendUsd, isOverBudget) are zero-valued
// today — they need P1-01 (LLM call logging) + P1-02 (price table) +
// the user_daily_rollup worker. The fields exist now so the wire
// shape doesn't change when those land.
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
	Last7dSpendUsd float64   `json:"last7dSpendUsd"` // 0 until P1-01 lands
	IsOverBudget   bool      `json:"isOverBudget"`   // false until P4-01 lands
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
	Sort   string // "-signup" | "-last_active" | "-uploads" | "-outfits"
	Cursor string // pagination cursor (createdAt timestamp encoded)
	Limit  int    // 1..100; default 20
}

// UsersRepository handles cross-collection reads needed by the admin
// users list. It reads the existing user/wardrobe/outfit/moodboard
// collections — never writes them.
type UsersRepository interface {
	ListSummaries(ctx context.Context, q UsersQuery) ([]UserSummary, string, error)
}

// UsersMongoRepository is the production implementation.
//
// We deliberately don't use a single $lookup-heavy aggregation —
// it's brittle when collections evolve and slow on cold cache. Instead
// we paginate the users collection (the authoritative dimension) and
// fan out lightweight count queries per user. At the projected year-1
// scale (≤200 users / page) this is plenty fast and stays understandable.
type UsersMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewUsersMongoRepository constructs a UsersMongoRepository.
func NewUsersMongoRepository(client *mongo.Client, dbName string) *UsersMongoRepository {
	return &UsersMongoRepository{client: client, dbName: dbName}
}

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

// ListSummaries returns one page of user summaries plus the cursor for
// the next page. Cursor encoding: empty for the first page, last user's
// _id for subsequent pages (we sort by createdAt desc + _id desc as
// tiebreaker to make the order stable).
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
	if q.Cursor != "" {
		// Cursor is the previous page's last user _id. We assume the
		// sort is createdAt-desc; pull rows older than that one.
		filter["_id"] = bson.M{"$lt": q.Cursor}
	}

	sort := bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}
	if q.Sort == "-last_active" {
		sort = bson.D{{Key: "updatedAt", Value: -1}, {Key: "_id", Value: -1}}
	}

	cur, err := r.usersCol().Find(ctx, filter,
		options.Find().SetSort(sort).SetLimit(int64(limit+1))) // +1 to know if there's another page
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

	summaries := make([]UserSummary, 0, len(docs))
	weekAgo := time.Now().UTC().Add(-7 * 24 * time.Hour)
	for _, d := range docs {
		s := UserSummary{
			ID:           d.ID,
			Email:        d.Email,
			Name:         d.Name,
			SignupDate:   d.CreatedAt,
			LastActiveAt: d.UpdatedAt,
		}

		// Per-user counts. These are tiny indexed point queries; at
		// 20 users/page that's 80 ops per request — comfortably under
		// 100ms total against local Mongo.
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

	nextCursor := ""
	if hasMore && len(summaries) > 0 {
		nextCursor = summaries[len(summaries)-1].ID
	}
	return summaries, nextCursor, nil
}

// errUnsupportedSort returned when the caller passes a sort key we
// haven't mapped. Public so handlers can format the response uniformly.
var errUnsupportedSort = errors.New("admin: unsupported sort key")
