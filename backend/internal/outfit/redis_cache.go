package outfit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements the Cache interface using Redis.
// Much faster than MongoDB for high-frequency cache reads (~0.5ms vs ~5ms).
type RedisCache struct {
	client *redis.Client
	ttl    time.Duration
	prefix string
}

func NewRedisCache(client *redis.Client, ttl time.Duration) *RedisCache {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &RedisCache{client: client, ttl: ttl, prefix: "outfit:cache:"}
}

func (c *RedisCache) Get(ctx context.Context, key string) ([]Outfit, bool) {
	data, err := c.client.Get(ctx, c.prefix+key).Bytes()
	if err != nil {
		return nil, false
	}
	var outfits []Outfit
	if err := json.Unmarshal(data, &outfits); err != nil {
		return nil, false
	}
	return outfits, true
}

func (c *RedisCache) Set(ctx context.Context, key string, outfits []Outfit) {
	data, err := json.Marshal(outfits)
	if err != nil {
		return
	}
	c.client.Set(ctx, c.prefix+key, data, c.ttl)
}
