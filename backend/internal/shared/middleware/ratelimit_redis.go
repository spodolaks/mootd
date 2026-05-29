package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRateLimit returns middleware that limits requests per user (or per IP for
// unauthenticated requests) using Redis INCR + EXPIRE under the "global" scope.
// Kept for backwards compatibility; new code should prefer RedisRateLimitScoped.
func RedisRateLimit(client *redis.Client, maxRequests int, window time.Duration) func(http.Handler) http.Handler {
	return RedisRateLimitScoped(client, "global", maxRequests, window)
}

// RedisRateLimitScoped is like RedisRateLimit but isolates the counter under the
// given scope so multiple limiters can coexist (e.g. a global 300/min per user
// plus an outfit-generate 5/min per user). The scope is a short, stable
// identifier — use "outfit:generate", "auth", etc.
func RedisRateLimitScoped(client *redis.Client, scope string, maxRequests int, window time.Duration) func(http.Handler) http.Handler {
	// Cache window seconds as string to save an alloc per request.
	windowSeconds := int(window.Seconds())
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := rateLimitKey(r)
			redisKey := fmt.Sprintf("ratelimit:%s:%s", scope, key)
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
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", windowSeconds))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// rateLimitKey picks the most specific identifier available for the request:
// authenticated user ID when present, else the client IP. The IP comes from
// clientIP, which honors X-Forwarded-For only from a trusted proxy — so an
// unauthenticated caller can't forge XFF to mint a fresh counter per request
// and walk past the auth/brute-force limiter. Used by both per-scope and
// global Redis limiters so the identity rules are consistent.
func rateLimitKey(r *http.Request) string {
	if userID, ok := UserIDFromContext(r.Context()); ok && userID != "" {
		return "user:" + userID
	}
	if ip := clientIP(r); ip != nil {
		return "ip:" + ip.String()
	}
	return "ip:" + r.RemoteAddr
}
