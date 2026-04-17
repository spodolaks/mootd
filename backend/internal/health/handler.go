// Package health provides liveness and readiness check endpoints.
package health

import (
	"context"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"mootd/backend/internal/shared/response"
)

// Handler handles health check endpoints.
type Handler struct {
	logger  *log.Logger
	db      *mongo.Client
	mongodb string
}

// NewHandler creates a new health Handler.
func NewHandler(logger *log.Logger, db *mongo.Client, mongodb string) *Handler {
	return &Handler{logger: logger, db: db, mongodb: mongodb}
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
// Returns 200 when MongoDB is reachable, 503 otherwise.
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

	response.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "ready",
		"service": "mootd-backend",
		"mongo":   h.mongodb,
	})
}
