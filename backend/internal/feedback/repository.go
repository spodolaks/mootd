package feedback

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Repository persists feedback events. It exposes only the operations needed
// for the append-only event log: insert a new event, list a user's events
// (useful for export / DSAR), and bulk-delete all events for a user on
// GDPR erasure.
type Repository interface {
	Insert(ctx context.Context, event Event) error
	ListByUser(ctx context.Context, userID string, limit int) ([]Event, error)
	DeleteAllByUser(ctx context.Context, userID string) (int, error)
}

// MongoRepository implements Repository against MongoDB.
type MongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoRepository creates a MongoRepository.
func NewMongoRepository(client *mongo.Client, dbName string) *MongoRepository {
	return &MongoRepository{client: client, dbName: dbName}
}

func (r *MongoRepository) collection() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("outfit_feedback")
}

// Insert appends a new event. CreatedAt and SchemaVersion are set here if the
// caller left them zero so every row in the collection is self-describing.
func (r *MongoRepository) Insert(ctx context.Context, event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.SchemaVersion == 0 {
		event.SchemaVersion = CurrentSchemaVersion
	}
	_, err := r.collection().InsertOne(ctx, event)
	return err
}

// ListByUser returns the most recent events for the user, newest first.
// Intended for the GDPR export flow and future prompt-building lookups.
func (r *MongoRepository) ListByUser(ctx context.Context, userID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(int64(limit))
	cursor, err := r.collection().Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var events []Event
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}

// DeleteAllByUser removes every event owned by userID. Wired into the user
// deletion cascade so GDPR erasure is total.
func (r *MongoRepository) DeleteAllByUser(ctx context.Context, userID string) (int, error) {
	res, err := r.collection().DeleteMany(ctx, bson.M{"userId": userID})
	if err != nil {
		return 0, err
	}
	return int(res.DeletedCount), nil
}
