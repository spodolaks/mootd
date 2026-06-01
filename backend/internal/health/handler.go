// Package health provides liveness and readiness check endpoints.
package health

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"mootd/backend/internal/buildinfo"
	"mootd/backend/internal/shared/response"
)

// Handler handles health check endpoints.
type Handler struct {
	logger       *log.Logger
	db           *mongo.Client
	mongodb      string
	redis        *redis.Client // optional; nil in dev-without-Redis mode
	requireRedis bool          // /readyz returns 503 if Redis is required + unreachable
}

// NewHandler creates a new health Handler.
func NewHandler(logger *log.Logger, db *mongo.Client, mongodb string) *Handler {
	return &Handler{logger: logger, db: db, mongodb: mongodb}
}

// WithRedis attaches an optional Redis client + a "is Redis
// required?" flag (mootd#55). When `required=true`, /readyz
// returns 503 with reason=`redis_unreachable` if the Redis ping
// fails. When false, Redis status is reported in the body but
// doesn't fail readiness — useful for dev/ENVIRONMENT≠production.
//
// Caller decides the policy at boot: in production (`ENVIRONMENT
// =production`), Redis is required because every fallback path
// degrades behaviour (rate-limit slips to in-memory, outfit
// cache vanishes, async jobs become local-only).
func (h *Handler) WithRedis(client *redis.Client, required bool) *Handler {
	h.redis = client
	h.requireRedis = required
	return h
}

// ClientHealthResponse is the wire shape of GET /v1/health
// (mootd#40). Polled by the RN app on foreground to detect
// breaking-change deploys + scheduled maintenance.
type ClientHealthResponse struct {
	Version          string `json:"version"`
	SHA              string `json:"sha"`
	MinClientVersion string `json:"minClientVersion"`
	Maintenance      bool   `json:"maintenance"`
}

// Healthz handles GET /healthz.
// Returns 200 without checking dependencies — indicates the process is alive.
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"service":  "mootd-backend",
		"time_utc": time.Now().UTC().Format(time.RFC3339),
	})
}

// Readyz handles GET /readyz.
//
// Returns 200 when every required dependency is reachable.
// Mongo is always required; Redis is required when
// WithRedis(client, true) was called at boot (mootd#55).
//
// Body always includes a `redis` field with the current state
// ("up", "down", or "absent") so operators can see the
// degradation mode without a separate metrics scrape.
func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx, readpref.Primary()); err != nil {
		h.logger.Printf("readiness ping failed: %v", err)
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":  "not_ready",
			"service": "mootd-backend",
			"reason":  "mongo_unreachable",
		})
		return
	}

	redisState := "absent"
	if h.redis != nil {
		if err := h.redis.Ping(ctx).Err(); err != nil {
			redisState = "down"
			if h.requireRedis {
				h.logger.Printf("readiness: redis required but unreachable: %v", err)
				response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status":  "not_ready",
					"service": "mootd-backend",
					"reason":  "redis_unreachable",
					"mongo":   h.mongodb,
					"redis":   redisState,
				})
				return
			}
		} else {
			redisState = "up"
		}
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "ready",
		"service": "mootd-backend",
		"mongo":   h.mongodb,
		"redis":   redisState,
	})
}

// ClientHealth handles GET /v1/health (mootd#40). Returns the
// running backend's identity + the minimum client version it
// expects. Polled by the RN app on foreground / once per session;
// the app renders a blocking "please update" sheet when its
// own version is below `minClientVersion`, and a soft banner
// when `maintenance: true`.
//
// Configuration is via env (no DB roundtrip — this endpoint
// must be reachable even when Mongo is degraded):
//
//	MIN_CLIENT_VERSION  — bumped by hand when shipping a
//	                      breaking change. Defaults to "0.0.0"
//	                      (every version is accepted).
//	MAINTENANCE         — "true" to advertise a maintenance
//	                      window. Defaults to false.
//
// Auth-optional. The endpoint already lives behind the global
// rate limiter; pin clients to one fetch per foreground.
func (h *Handler) ClientHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	minClient := os.Getenv("MIN_CLIENT_VERSION")
	if minClient == "" {
		minClient = "0.0.0"
	}
	maintenance := strings.EqualFold(os.Getenv("MAINTENANCE"), "true")

	response.WriteJSON(w, http.StatusOK, ClientHealthResponse{
		Version:          buildinfo.Version,
		SHA:              buildinfo.SHA,
		MinClientVersion: minClient,
		Maintenance:      maintenance,
	})
}
