package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitIncrScript atomically increments the counter and, only on the first
// increment (when the post-INCR value is 1), sets the window TTL via PEXPIRE.
// Doing both in one EVAL closes the INCR-then-EXPIRE race: a process/Redis
// hiccup (or two interleaved requests) can no longer leave the key without a
// TTL, which would otherwise pin the counter forever and permanently
// rate-limit the user. KEYS[1] = counter key, ARGV[1] = window in
// milliseconds. Returns the post-increment count.
var rateLimitIncrScript = redis.NewScript(`
local n = redis.call("INCR", KEYS[1])
if n == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return n
`)

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
	// PEXPIRE takes milliseconds; precompute once so the per-request hot path
	// only formats the key.
	windowMillis := window.Milliseconds()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := rateLimitKey(r)
			redisKey := fmt.Sprintf("ratelimit:%s:%s", scope, key)
			ctx := r.Context()

			// Atomic INCR + (first-increment-only) PEXPIRE via Lua so the
			// counter always gets a TTL — see rateLimitIncrScript.
			count, err := rateLimitIncrScript.Run(ctx, client, []string{redisKey}, windowMillis).Int64()
			if err != nil {
				// Redis down — allow the request (fail open)
				next.ServeHTTP(w, r)
				return
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
