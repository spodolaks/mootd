package events

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Repository is the persistence boundary.
type Repository interface {
	AppendBatch(ctx context.Context, events []Event) error
}

// MongoRepository writes to the `events` collection. Single
// collection by design — partitioning by event name would
// help if any one event's volume dominated, but at our
// projected year-1 volume (~1M events / month total across
// ~5 high-volume names) a single time-series-friendly
// collection with the right indexes is simpler.
type MongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoRepository ensures the indexes the read surfaces in
// later issues will lean on:
//
//   - (createdAt desc)            — firehose / time-range queries
//   - (userId, createdAt desc)    — per-user feed (P2-03 admin UI)
//   - (name, createdAt desc)      — per-event-name slices
//     (P2-04 funnels, P2-05 cohorts)
//   - (sessionId)                 — session aggregation
func NewMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*MongoRepository, error) {
	r := &MongoRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("events_created_desc"),
		},
		{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("events_user_created_desc"),
		},
		{
			Keys:    bson.D{{Key: "name", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName("events_name_created_desc"),
		},
		{
			Keys:    bson.D{{Key: "sessionId", Value: 1}},
			Options: options.Index().SetName("events_session"),
		},
	}); err != nil {
		return nil, fmt.Errorf("ensure events indexes: %w", err)
	}
	return r, nil
}

func (r *MongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("events")
}

// AppendBatch inserts every event in one call. We use
// `InsertMany` with `ordered: false` so a single bad insert
// (e.g. duplicate _id on a retry) doesn't fail the batch —
// matches the issue's "invalid events don't poison the batch"
// acceptance, applied at the storage layer too.
func (r *MongoRepository) AppendBatch(ctx context.Context, evts []Event) error {
	if len(evts) == 0 {
		return nil
	}
	docs := make([]any, 0, len(evts))
	for _, e := range evts {
		docs = append(docs, e)
	}
	_, err := r.col().InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	// Mongo returns ErrBulkWriteException with per-doc errors
	// when ordered:false; for the ingest path we swallow them
	// (the caller already validated; storage failures are
	// observability-only at this layer).
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}
