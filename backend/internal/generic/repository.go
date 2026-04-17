package generic

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/shared/id"
)

// Repository handles generic item persistence.
type Repository interface {
	// FindOrCreate returns an existing generic item by dedup key, or creates a new one.
	FindOrCreate(ctx context.Context, item GenericItem) (*GenericItem, bool, error)
	// FindMatching returns generic items compatible with the given archetype scores.
	FindMatching(ctx context.Context, scores archetype.Scores, excludeCategories map[string]int, limit int) ([]GenericItem, error)
	// IncrementUsage bumps the usage counter for a generic item.
	IncrementUsage(ctx context.Context, itemID string) error
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
	return r.client.Database(r.dbName).Collection("generic_items")
}

// DedupKey computes a deterministic key for deduplication.
func DedupKey(primaryArchetype, category, label string) string {
	raw := strings.ToLower(fmt.Sprintf("%s/%s/%s", primaryArchetype, category, label))
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash[:16]) // 32-char hex
}

// FindOrCreate returns an existing item if the dedup key exists (and increments usage),
// otherwise inserts a new item. Returns (item, created, error).
func (r *MongoRepository) FindOrCreate(ctx context.Context, item GenericItem) (*GenericItem, bool, error) {
	if item.ID == "" {
		item.ID = id.Generate()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}

	// Try to find existing.
	var existing GenericItem
	err := r.collection().FindOne(ctx, bson.M{"dedupKey": item.DedupKey}).Decode(&existing)
	if err == nil {
		// Exists — increment usage and return. Best-effort: log but don't fail the request.
		if _, updateErr := r.collection().UpdateOne(ctx, bson.M{"_id": existing.ID}, bson.M{"$inc": bson.M{"usageCount": 1}}); updateErr != nil {
			log.Printf("generic: usage increment failed for item %s: %v", existing.ID, updateErr)
		}
		existing.UsageCount++
		return &existing, false, nil
	}
	if err != mongo.ErrNoDocuments {
		return nil, false, err
	}

	// Insert new.
	item.UsageCount = 1
	_, err = r.collection().InsertOne(ctx, item)
	if err != nil {
		// Race condition: another goroutine inserted between find and insert.
		if mongo.IsDuplicateKeyError(err) {
			return r.FindOrCreate(ctx, item) // retry
		}
		return nil, false, err
	}
	return &item, true, nil
}

// FindMatching returns generic items compatible with the user's top archetypes.
// excludeCategories maps category → count; categories with count >= 2 are excluded.
func (r *MongoRepository) FindMatching(
	ctx context.Context,
	scores archetype.Scores,
	excludeCategories map[string]int,
	limit int,
) ([]GenericItem, error) {
	top := archetype.TopN(scores, 3)
	if len(top) == 0 {
		return nil, nil
	}

	archetypeNames := make([]string, len(top))
	for i, a := range top {
		archetypeNames[i] = a.Name
	}

	// Build category exclusion list.
	var excludedCats []string
	for cat, count := range excludeCategories {
		if count >= 2 {
			excludedCats = append(excludedCats, cat)
		}
	}

	filter := bson.M{"primaryArchetype": bson.M{"$in": archetypeNames}}
	if len(excludedCats) > 0 {
		filter["category"] = bson.M{"$nin": excludedCats}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "usageCount", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []GenericItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// IncrementUsage bumps the usage counter for a generic item.
func (r *MongoRepository) IncrementUsage(ctx context.Context, itemID string) error {
	_, err := r.collection().UpdateOne(ctx, bson.M{"_id": itemID}, bson.M{"$inc": bson.M{"usageCount": 1}})
	return err
}
