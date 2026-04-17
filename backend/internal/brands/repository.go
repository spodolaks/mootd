package brands

import (
	"context"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const maxSearchResults = 20

// Repository handles brand persistence.
type Repository interface {
	// Save upserts a brand by its lowercase name. First write wins for display name.
	Save(ctx context.Context, displayName string) error
	// Search returns display names that contain the query (case-insensitive), up to 20 results.
	Search(ctx context.Context, query string) ([]string, error)
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
	return r.client.Database(r.dbName).Collection("brands")
}

// Save upserts the brand. The _id is the lowercase name for deduplication.
// displayName is preserved from the first insert ($setOnInsert).
func (r *MongoRepository) Save(ctx context.Context, displayName string) error {
	id := strings.ToLower(strings.TrimSpace(displayName))
	if id == "" {
		return nil
	}

	filter := bson.M{"_id": id}
	update := bson.M{
		"$setOnInsert": bson.M{
			"displayName": strings.TrimSpace(displayName),
			"createdAt":   time.Now().UTC(),
		},
	}
	_, err := r.collection().UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	return err
}

// Search returns brand display names whose lowercase form contains the query.
// Results are sorted alphabetically and capped at maxSearchResults.
func (r *MongoRepository) Search(ctx context.Context, query string) ([]string, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return []string{}, nil
	}

	filter := bson.M{"_id": bson.M{"$regex": regexp.QuoteMeta(q), "$options": "i"}}
	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(maxSearchResults)

	cursor, err := r.collection().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var brands []Brand
	if err := cursor.All(ctx, &brands); err != nil {
		return nil, err
	}

	names := make([]string, len(brands))
	for i, b := range brands {
		names[i] = b.DisplayName
	}
	return names, nil
}
