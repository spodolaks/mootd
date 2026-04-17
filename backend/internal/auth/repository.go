package auth

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// UserDocument is the MongoDB representation of a user.
type UserDocument struct {
	ID               string    `bson:"_id"`
	Email            string    `bson:"email"`
	Name             string    `bson:"name"`
	AvatarURL        string    `bson:"avatarUrl"`
	GoogleID         string    `bson:"googleId"`
	RefreshTokenHash string    `bson:"refreshTokenHash,omitempty"`
	RefreshExpiresAt time.Time `bson:"refreshExpiresAt,omitempty"`
	CreatedAt        time.Time `bson:"createdAt"`
	UpdatedAt        time.Time `bson:"updatedAt"`
}

// Repository handles user persistence for authentication.
type Repository interface {
	UpsertByGoogleID(ctx context.Context, googleID, email, name, avatarURL string) error
	SaveRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	FindByRefreshToken(ctx context.Context, tokenHash string) (*UserDocument, error)
	ClearRefreshToken(ctx context.Context, userID string) error
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

// UpsertByGoogleID creates or updates a user identified by their Google sub ID.
// Uses time.Time for timestamps to preserve native MongoDB date query support.
func (r *MongoRepository) UpsertByGoogleID(ctx context.Context, googleID, email, name, avatarURL string) error {
	now := time.Now().UTC()
	filter := bson.M{"googleId": googleID}
	update := bson.M{
		"$set": bson.M{
			"email":     email,
			"name":      name,
			"avatarUrl": avatarURL,
			"googleId":  googleID,
			"updatedAt": now,
		},
		"$setOnInsert": bson.M{
			"_id":       googleID,
			"createdAt": now,
		},
	}
	_, err := r.collection().UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	return err
}

// SaveRefreshToken stores a hashed refresh token and its expiry on the user document.
func (r *MongoRepository) SaveRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	filter := bson.M{"_id": userID}
	update := bson.M{
		"$set": bson.M{
			"refreshTokenHash": tokenHash,
			"refreshExpiresAt": expiresAt,
		},
	}
	_, err := r.collection().UpdateOne(ctx, filter, update)
	return err
}

// FindByRefreshToken looks up a user by their hashed refresh token, returning nil if
// no matching non-expired token is found.
func (r *MongoRepository) FindByRefreshToken(ctx context.Context, tokenHash string) (*UserDocument, error) {
	filter := bson.M{
		"refreshTokenHash": tokenHash,
		"refreshExpiresAt": bson.M{"$gt": time.Now().UTC()},
	}
	var doc UserDocument
	if err := r.collection().FindOne(ctx, filter).Decode(&doc); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// ClearRefreshToken removes the refresh token hash and expiry from a user document.
func (r *MongoRepository) ClearRefreshToken(ctx context.Context, userID string) error {
	filter := bson.M{"_id": userID}
	update := bson.M{
		"$unset": bson.M{
			"refreshTokenHash": "",
			"refreshExpiresAt": "",
		},
	}
	_, err := r.collection().UpdateOne(ctx, filter, update)
	return err
}
