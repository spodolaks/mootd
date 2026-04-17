package outfit

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoCache is a 24-hour TTL cache for outfit suggestions, keyed by a hash of
// the user, wardrobe membership, weather bucket, and top archetypes. It lets
// repeated "regenerate" taps within a day return the same set of outfits
// without re-paying the LLM cost.
//
// The collection should be created with a TTL index on `expiresAt` so old
// entries are reaped automatically. The handler creates the index lazily on
// first use.
type MongoCache struct {
	client *mongo.Client
	dbName string
	ttl    time.Duration
	logger *log.Logger
}

// NewMongoCache constructs a MongoCache. ttl defaults to 24h when zero.
func NewMongoCache(client *mongo.Client, dbName string, ttl time.Duration, logger *log.Logger) *MongoCache {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	c := &MongoCache{client: client, dbName: dbName, ttl: ttl, logger: logger}
	c.ensureTTLIndex(context.Background())
	return c
}

func (c *MongoCache) collection() *mongo.Collection {
	return c.client.Database(c.dbName).Collection("outfit_cache")
}

// ensureTTLIndex creates the expiresAt TTL index if it does not yet exist.
// MongoDB ignores duplicate index requests so this is safe to call repeatedly.
func (c *MongoCache) ensureTTLIndex(ctx context.Context) {
	indexCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	model := mongo.IndexModel{
		Keys:    bson.D{{Key: "expiresAt", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0).SetName("outfit_cache_ttl"),
	}
	if _, err := c.collection().Indexes().CreateOne(indexCtx, model); err != nil {
		c.logger.Printf("outfit cache: ensure TTL index: %v", err)
	}
}

// outfitCacheDoc is the persisted shape for one cache entry.
type outfitCacheDoc struct {
	Key       string    `bson:"_id"`
	Outfits   []Outfit  `bson:"outfits"`
	ExpiresAt time.Time `bson:"expiresAt"`
}

// Get returns the cached outfits for the key, or (nil, false) on miss/expired.
func (c *MongoCache) Get(ctx context.Context, key string) ([]Outfit, bool) {
	var doc outfitCacheDoc
	err := c.collection().FindOne(ctx, bson.M{"_id": key}).Decode(&doc)
	if err != nil {
		return nil, false
	}
	if time.Now().UTC().After(doc.ExpiresAt) {
		// Expired but TTL reaper hasn't run yet — treat as a miss.
		return nil, false
	}
	return doc.Outfits, true
}

// Set stores outfits under the key with a TTL of c.ttl from now.
// Errors are logged but never propagated — the cache is best-effort.
func (c *MongoCache) Set(ctx context.Context, key string, outfits []Outfit) {
	doc := outfitCacheDoc{
		Key:       key,
		Outfits:   outfits,
		ExpiresAt: time.Now().UTC().Add(c.ttl),
	}
	_, err := c.collection().ReplaceOne(ctx, bson.M{"_id": key}, doc, options.Replace().SetUpsert(true))
	if err != nil {
		c.logger.Printf("outfit cache: set %s: %v", key, err)
	}
}
