package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRateLimit returns middleware that limits requests per user (or per IP for
// unauthenticated requests) using Redis INCR + EXPIRE.
func RedisRateLimit(client *redis.Client, maxRequests int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prefer user ID from context (set by auth middleware), fall back to IP.
			key := r.RemoteAddr
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				key = forwarded
			}
			if userID, ok := UserIDFromContext(r.Context()); ok {
				key = "user:" + userID
			}

			redisKey := fmt.Sprintf("ratelimit:%s", key)
			ctx := context.Background()

			count, err := client.Incr(ctx, redisKey).Result()
			if err != nil {
				// Redis down — allow the request (fail open)
				next.ServeHTTP(w, r)
				return
			}
			if count == 1 {
				client.Expire(ctx, redisKey, window)
			}

			if count > int64(maxRequests) {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
