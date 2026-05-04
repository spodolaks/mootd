package user

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Repository abstracts user persistence operations.
type Repository interface {
	FindByID(ctx context.Context, id string) (*UserDocument, error)
	Update(ctx context.Context, id string, fields UpdateProfileRequest) (*UserDocument, error)
	UpdateArchetypeProfile(ctx context.Context, id string, profile map[string]float64) error
	// DeleteByID removes the user document. Used for GDPR account erasure.
	DeleteByID(ctx context.Context, id string) error
}

// MongoRepository implements Repository using MongoDB.
type MongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoRepository creates a MongoRepository.
func NewMongoRepository(client *mongo.Client, dbName string) *MongoRepository {
	return &MongoRepository{client: client, dbName: dbName}
}

func (r *MongoRepository) collection() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("users")
}

// FindByID retrieves a user by their ID (_id field).
// Returns nil, mongo.ErrNoDocuments when the user does not exist.
func (r *MongoRepository) FindByID(ctx context.Context, id string) (*UserDocument, error) {
	var doc UserDocument
	if err := r.collection().FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Update applies non-nil fields from req to the user record and returns the updated document.
func (r *MongoRepository) Update(ctx context.Context, id string, req UpdateProfileRequest) (*UserDocument, error) {
	updates := bson.M{"updatedAt": time.Now().UTC().Format(time.RFC3339)}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.AvatarURL != nil {
		updates["avatarUrl"] = *req.AvatarURL
	}
	if req.Creativity != nil {
		// mootd#67 — clamp to [0, 1]. The slider can't escape
		// that range on the client, but a hand-crafted curl
		// could; refuse silently rather than 400 since we're
		// applying the safe fallback.
		c := *req.Creativity
		if c < 0 {
			c = 0
		}
		if c > 1 {
			c = 1
		}
		updates["creativity"] = c
	}

	if len(updates) == 1 {
		return nil, errors.New("no fields to update")
	}

	result, err := r.collection().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": updates})
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, mongo.ErrNoDocuments
	}

	return r.FindByID(ctx, id)
}

// DeleteByID removes the user document identified by id.
// Absence is treated as success so erasure is idempotent.
func (r *MongoRepository) DeleteByID(ctx context.Context, id string) error {
	_, err := r.collection().DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// UpdateArchetypeProfile sets the archetype profile scores on the user document.
func (r *MongoRepository) UpdateArchetypeProfile(ctx context.Context, id string, profile map[string]float64) error {
	_, err := r.collection().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"archetypeProfile": profile,
			"updatedAt":        time.Now().UTC().Format(time.RFC3339),
		},
	})
	return err
}
