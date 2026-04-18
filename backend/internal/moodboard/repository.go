package moodboard

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"mootd/backend/internal/shared/pagination"
)

// Repository handles moodboard persistence.
type Repository interface {
	Save(ctx context.Context, board SavedMoodBoard) error
	FindByUser(ctx context.Context, userID string) ([]SavedMoodBoard, error)
	// FindByUserPaginated returns a page of moodboards using cursor-based pagination.
	FindByUserPaginated(ctx context.Context, userID string, limit int, cursor *pagination.Cursor) ([]SavedMoodBoard, error)
	// FindRecent returns the N most recent moodboards for the user, newest first.
	FindRecent(ctx context.Context, userID string, limit int) ([]SavedMoodBoard, error)
	// DeleteAllByUser removes every moodboard owned by userID. Used for GDPR
	// account erasure.
	DeleteAllByUser(ctx context.Context, userID string) (int, error)
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
	return r.client.Database(r.dbName).Collection("moodboards")
}

// Save upserts a moodboard for the given user+date.
// If an outfit already exists for that date, the old one is deleted first
// to avoid MongoDB's immutable _id constraint on ReplaceOne.
func (r *MongoRepository) Save(ctx context.Context, board SavedMoodBoard) error {
	filter := bson.M{"userId": board.UserID, "date": board.Date}
	if _, err := r.collection().DeleteMany(ctx, filter); err != nil {
		return fmt.Errorf("moodboard: purge existing for user %s date %s: %w", board.UserID, board.Date, err)
	}
	_, err := r.collection().InsertOne(ctx, board)
	return err
}

// FindByUser returns all moodboards for the user, newest first.
func (r *MongoRepository) FindByUser(ctx context.Context, userID string) ([]SavedMoodBoard, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection().Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var boards []SavedMoodBoard
	if err := cursor.All(ctx, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

// FindByUserPaginated returns a page of moodboards for the given user.
// It fetches limit+1 rows so the caller can detect whether a next page exists.
func (r *MongoRepository) FindByUserPaginated(ctx context.Context, userID string, limit int, cursor *pagination.Cursor) ([]SavedMoodBoard, error) {
	filter := bson.M{"userId": userID}
	filter = pagination.BuildFilter(filter, cursor)

	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit + 1))

	cur, err := r.collection().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var boards []SavedMoodBoard
	if err := cur.All(ctx, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

// DeleteAllByUser removes every moodboard owned by userID.
func (r *MongoRepository) DeleteAllByUser(ctx context.Context, userID string) (int, error) {
	res, err := r.collection().DeleteMany(ctx, bson.M{"userId": userID})
	if err != nil {
		return 0, err
	}
	return int(res.DeletedCount), nil
}

// FindRecent returns the N most recent moodboards for the user, newest first.
func (r *MongoRepository) FindRecent(ctx context.Context, userID string, limit int) ([]SavedMoodBoard, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(int64(limit))
	cursor, err := r.collection().Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var boards []SavedMoodBoard
	if err := cursor.All(ctx, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

