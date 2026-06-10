// Package db contains database connection logic.
package db

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// ConnectMongo establishes a connection to MongoDB and pings it to verify connectivity.
// It returns an error if the connection fails or if the ping fails.
func ConnectMongo(ctx context.Context, uri string) (*mongo.Client, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}

	return client, nil
}

// EnsureIndexes creates indexes required for query performance.
// Safe to call on every startup — MongoDB skips existing indexes.
func EnsureIndexes(ctx context.Context, client *mongo.Client, dbName string, logger *log.Logger) {
	db := client.Database(dbName)

	indexes := []struct {
		collection string
		model      mongo.IndexModel
	}{
		{
			collection: "wardrobe_items",
			model: mongo.IndexModel{
				Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			},
		},
		{
			collection: "moodboards",
			model: mongo.IndexModel{
				Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			},
		},
		{
			collection: "moodboards",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "date", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "users",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "googleId", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		// Refresh-token lookups (POST /v1/auth/refresh, logout) filter users by
		// refreshTokenHash; without this index every refresh is a COLLSCAN.
		// Sparse so it only covers logged-in users — logged-out users have the
		// field $unset, so they stay out of the index.
		{
			collection: "users",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "refreshTokenHash", Value: 1}},
				Options: options.Index().SetSparse(true),
			},
		},
		// Generic items pool
		{
			collection: "generic_items",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "dedupKey", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "generic_items",
			model: mongo.IndexModel{
				Keys: bson.D{{Key: "primaryArchetype", Value: 1}, {Key: "category", Value: 1}},
			},
		},
		{
			collection: "generic_items",
			model: mongo.IndexModel{
				Keys: bson.D{{Key: "usageCount", Value: -1}},
			},
		},
		{
			collection: "wardrobe_items",
			model: mongo.IndexModel{
				Keys: bson.D{{Key: "imageUrl", Value: 1}, {Key: "pngImageUrl", Value: 1}},
			},
		},
		// Feedback events are queried by user (export/DSAR) and scanned by
		// (userId, createdAt) descending for "recent preference" prompt
		// lookups.
		{
			collection: "outfit_feedback",
			model: mongo.IndexModel{
				Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			},
		},
	}

	for _, idx := range indexes {
		coll := db.Collection(idx.collection)
		name, err := coll.Indexes().CreateOne(ctx, idx.model)
		if err != nil {
			logger.Printf("WARNING: failed to create index on %s: %v", idx.collection, err)
		} else {
			logger.Printf("index ensured: %s.%s", idx.collection, name)
		}
	}
}
