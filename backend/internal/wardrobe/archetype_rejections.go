package wardrobe

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ArchetypeRejection records that a user has explicitly said
// "this archetype-default isn't actually in my wardrobe" — so the
// outfit-generation pool can stop offering it back to them.
//
// Lives in wardrobe/ rather than admin/ because the data is
// per-user and read by the runtime outfit pipeline (which can
// import wardrobe but not admin under the codebase's one-way dep
// convention). The admin curator never reads this — it's purely
// a per-user filter applied at filler-load time.
type ArchetypeRejection struct {
	UserID    string    `bson:"userId"`
	DefaultID string    `bson:"defaultId"`
	CreatedAt time.Time `bson:"createdAt"`
}

// ArchetypeRejectionsRepository is the persistence contract.
type ArchetypeRejectionsRepository interface {
	// Add is idempotent — re-rejecting the same default is a
	// no-op rather than a duplicate-key error so the FE can
	// fire-and-forget.
	Add(ctx context.Context, userID, defaultID string) error
	// ListIDs returns the set of defaultIds the user has
	// rejected, suitable for a Mongo $nin filter.
	ListIDs(ctx context.Context, userID string) ([]string, error)
	// Delete clears one user's rejection of one default. Called
	// after a successful "I have this IRL" claim (mootd#75) so
	// the user's reject list doesn't leak stale entries for
	// items they now own. Idempotent — deleting a row that isn't
	// there returns nil.
	Delete(ctx context.Context, userID, defaultID string) error
}

// ArchetypeRejectionsMongoRepository is the production
// implementation. Single compound unique index on
// (userId, defaultId) is the entire schema — there's no other
// query shape we need.
type ArchetypeRejectionsMongoRepository struct {
	col *mongo.Collection
}

// NewArchetypeRejectionsMongoRepository ensures the unique index
// exists. Best-effort: on index-creation failure the repo still
// returns and Add will surface the duplicate as a Mongo error
// (which we'd swallow as already-rejected anyway).
func NewArchetypeRejectionsMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*ArchetypeRejectionsMongoRepository, error) {
	col := client.Database(dbName).Collection("archetype_rejections")
	_, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "defaultId", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_user_default"),
	})
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		// CreateOne returns CommandError IndexOptionsConflict when
		// the index already exists with a different name (rare).
		// Surface through; callers can decide.
		return nil, err
	}
	return &ArchetypeRejectionsMongoRepository{col: col}, nil
}

// Add inserts one rejection row, swallowing duplicate-key errors
// (idempotent — re-rejecting is a no-op).
func (r *ArchetypeRejectionsMongoRepository) Add(ctx context.Context, userID, defaultID string) error {
	if userID == "" || defaultID == "" {
		return errors.New("wardrobe: archetype rejection requires userID + defaultID")
	}
	_, err := r.col.InsertOne(ctx, ArchetypeRejection{
		UserID:    userID,
		DefaultID: defaultID,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil && mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

// Delete removes one user's rejection of one default. Idempotent —
// "delete a row that isn't there" returns nil. Used by the claim
// flow to clear stale rejections (mootd#75).
func (r *ArchetypeRejectionsMongoRepository) Delete(ctx context.Context, userID, defaultID string) error {
	if userID == "" || defaultID == "" {
		return errors.New("wardrobe: archetype rejection delete requires userID + defaultID")
	}
	_, err := r.col.DeleteOne(ctx, bson.M{"userId": userID, "defaultId": defaultID})
	return err
}

// ListIDs returns every defaultId this user has rejected.
func (r *ArchetypeRejectionsMongoRepository) ListIDs(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, errors.New("wardrobe: ListIDs requires userID")
	}
	cursor, err := r.col.Find(ctx, bson.M{"userId": userID},
		options.Find().SetProjection(bson.M{"defaultId": 1, "_id": 0}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var rows []struct {
		DefaultID string `bson:"defaultId"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.DefaultID
	}
	return out, nil
}
