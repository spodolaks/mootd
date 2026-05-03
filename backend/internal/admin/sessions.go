package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// Admin session replay (P5-05 / mootd-admin#38).
//
// Scoped tight: we record per-request *summaries* (method,
// path, status, duration), not DOM-replay events. Why:
//
//   - rrweb DOM replay would add ~30KB gzipped to every admin
//     page load, and we don't have a player UI for v1.
//   - Action-trail audit covers most of the "what did this
//     admin do?" question. Pages they visited = paths they
//     hit. PII reveals = already in admin_audit (P5-04).
//   - Stripping query strings client-side keeps the recording
//     itself out of the PII-handling regime — there's nothing
//     in a method+path+status that needs special protection.
//
// rrweb integration is the obvious follow-up if a real
// "I want to see exactly what they saw" replay is needed.
// Until then, this surface satisfies the audit-trail intent
// of the issue at a fraction of the cost.
// ────────────────────────────────────────────────────────────────────

const sessionEventsCollection = "admin_session_events"

// SessionEvent is one stored row.
type SessionEvent struct {
	ID         string    `bson:"_id"        json:"-"`
	SessionID  string    `bson:"sessionId"  json:"sessionId"`
	AdminID    string    `bson:"adminId"    json:"adminId"`
	At         time.Time `bson:"at"         json:"at"`
	Method     string    `bson:"method"     json:"method"`
	Path       string    `bson:"path"       json:"path"`
	Status     int       `bson:"status"     json:"status"`
	DurationMs int64     `bson:"durationMs" json:"durationMs"`
}

// SessionSummary mirrors the wire shape returned by the list
// endpoint.
type SessionSummary struct {
	SessionID     string    `json:"sessionId"`
	AdminID       string    `json:"adminId"`
	AdminEmail    string    `json:"adminEmail,omitempty"`
	FirstAt       time.Time `json:"firstAt"`
	LastAt        time.Time `json:"lastAt"`
	EventCount    int64     `json:"eventCount"`
	DistinctPaths int64     `json:"distinctPaths,omitempty"`
	ErrorCount    int64     `json:"errorCount,omitempty"`
}

// SessionDetail is the per-session response.
type SessionDetail struct {
	Summary SessionSummary `json:"summary"`
	Events  []SessionEvent `json:"events"`
}

// SessionsRepository is the persistence boundary.
type SessionsRepository interface {
	Append(ctx context.Context, e SessionEvent) error
	ListSummaries(ctx context.Context, cursor string, limit int) ([]SessionSummary, string, error)
	GetSession(ctx context.Context, sessionID string) (*SessionDetail, error)
}

// SessionsMongoRepository implements SessionsRepository with a
// 90-day TTL on the createdAt field.
type SessionsMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewSessionsMongoRepository creates indexes — TTL on `at` (90
// days), plus (sessionId, at) for fast detail lookups.
func NewSessionsMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*SessionsMongoRepository, error) {
	r := &SessionsMongoRepository{client: client, dbName: dbName}
	const ninetyDays = 90 * 24 * 60 * 60
	if _, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// TTL — Mongo deletes after 90 days. Storage stays bounded
			// without a daily cleanup job.
			Keys:    bson.D{{Key: "at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(ninetyDays).SetName("session_events_ttl"),
		},
		{
			// Per-session ordering for detail page.
			Keys:    bson.D{{Key: "sessionId", Value: 1}, {Key: "at", Value: 1}},
			Options: options.Index().SetName("session_events_session_at"),
		},
		{
			// Per-admin browsing on the list page.
			Keys:    bson.D{{Key: "adminId", Value: 1}, {Key: "at", Value: -1}},
			Options: options.Index().SetName("session_events_admin_at_desc"),
		},
	}); err != nil {
		return nil, fmt.Errorf("ensure session_events indexes: %w", err)
	}
	return r, nil
}

func (r *SessionsMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection(sessionEventsCollection)
}

func (r *SessionsMongoRepository) Append(ctx context.Context, e SessionEvent) error {
	if e.ID == "" {
		e.ID = generateAuditID() // reuse the audit ID format — content-free unique strings
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	_, err := r.col().InsertOne(ctx, e)
	return err
}

// ListSummaries aggregates events into per-session rows, sorted
// by lastAt desc. Cursor pagination on lastAt to keep the page
// stable when new events land.
func (r *SessionsMongoRepository) ListSummaries(ctx context.Context, cursor string, limit int) ([]SessionSummary, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	matchStage := bson.M{}
	if cursor != "" {
		// Cursor format: RFC-3339 lastAt. Anything strictly older
		// than the cursor is the next page.
		t, err := time.Parse(time.RFC3339Nano, cursor)
		if err == nil {
			matchStage["at"] = bson.M{"$lt": t}
		}
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchStage}},
		{{Key: "$group", Value: bson.M{
			"_id":           "$sessionId",
			"adminId":       bson.M{"$first": "$adminId"},
			"firstAt":       bson.M{"$min": "$at"},
			"lastAt":        bson.M{"$max": "$at"},
			"eventCount":    bson.M{"$sum": 1},
			"distinctPaths": bson.M{"$addToSet": "$path"},
			"errorCount":    bson.M{"$sum": bson.M{"$cond": []any{bson.M{"$gte": []any{"$status", 400}}, 1, 0}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "lastAt", Value: -1}}}},
		{{Key: "$limit", Value: int64(limit + 1)}},
	}

	cur, err := r.col().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	type aggRow struct {
		SessionID     string    `bson:"_id"`
		AdminID       string    `bson:"adminId"`
		FirstAt       time.Time `bson:"firstAt"`
		LastAt        time.Time `bson:"lastAt"`
		EventCount    int64     `bson:"eventCount"`
		DistinctPaths []string  `bson:"distinctPaths"`
		ErrorCount    int64     `bson:"errorCount"`
	}
	var rows []aggRow
	if err := cur.All(ctx, &rows); err != nil {
		return nil, "", err
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	out := make([]SessionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, SessionSummary{
			SessionID:     row.SessionID,
			AdminID:       row.AdminID,
			FirstAt:       row.FirstAt,
			LastAt:        row.LastAt,
			EventCount:    row.EventCount,
			DistinctPaths: int64(len(row.DistinctPaths)),
			ErrorCount:    row.ErrorCount,
		})
	}

	next := ""
	if hasMore && len(out) > 0 {
		next = out[len(out)-1].LastAt.Format(time.RFC3339Nano)
	}
	return out, next, nil
}

// GetSession returns the per-session summary + every event
// chronologically. Capped at 1000 events to bound the response
// size — sessions are typically <100 events; an admin who
// generates 1000+ is either testing or doing something wrong.
func (r *SessionsMongoRepository) GetSession(ctx context.Context, sessionID string) (*SessionDetail, error) {
	if sessionID == "" {
		return nil, errors.New("admin: sessionID required")
	}
	cur, err := r.col().Find(ctx,
		bson.M{"sessionId": sessionID},
		options.Find().SetSort(bson.D{{Key: "at", Value: 1}}).SetLimit(1000),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var events []SessionEvent
	if err := cur.All(ctx, &events); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}

	var (
		distinctPaths = map[string]struct{}{}
		errorCount    int64
	)
	for _, e := range events {
		distinctPaths[e.Path] = struct{}{}
		if e.Status >= 400 {
			errorCount++
		}
	}

	summary := SessionSummary{
		SessionID:     sessionID,
		AdminID:       events[0].AdminID,
		FirstAt:       events[0].At,
		LastAt:        events[len(events)-1].At,
		EventCount:    int64(len(events)),
		DistinctPaths: int64(len(distinctPaths)),
		ErrorCount:    errorCount,
	}
	return &SessionDetail{Summary: summary, Events: events}, nil
}

// ────────────────────────────────────────────────────────────────────
// HTTP handlers.
// ────────────────────────────────────────────────────────────────────

// RecordSessionEvent handles POST /admin/v1/sessions/events.
// Fire-and-forget from the FE. We don't block the FE on this
// call: failures are logged + 4xx/5xx swallowed by the FE's
// catch.
func (h *Handler) RecordSessionEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.sessionsRepo == nil {
		// Silent 204 — the FE doesn't need to know recording is
		// unwired; failing this would just spam its error toast.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	adminID, ok := AdminIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body struct {
		SessionID  string `json:"sessionId"`
		Method     string `json:"method"`
		Path       string `json:"path"`
		Status     int    `json:"status"`
		DurationMs int64  `json:"durationMs"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.SessionID == "" || body.Method == "" || body.Path == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "sessionId, method, path required"})
		return
	}

	// Defensively strip any query string the FE forgot to drop.
	if idx := strings.Index(body.Path, "?"); idx >= 0 {
		body.Path = body.Path[:idx]
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.sessionsRepo.Append(ctx, SessionEvent{
		SessionID:  body.SessionID,
		AdminID:    adminID,
		Method:     body.Method,
		Path:       body.Path,
		Status:     body.Status,
		DurationMs: body.DurationMs,
	}); err != nil {
		h.logger.Printf("admin /sessions/events: %v", err)
		// Still 204 — recording failures shouldn't surface to
		// the admin's UI. The error log is the operator's
		// cue.
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListSessions handles GET /admin/v1/sessions.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.sessionsRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session replay not wired"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	cursor := r.URL.Query().Get("cursor")
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}

	rows, next, err := h.sessionsRepo.ListSummaries(ctx, cursor, limit)
	if err != nil {
		h.logger.Printf("admin /sessions: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if rows == nil {
		rows = []SessionSummary{}
	}

	// Best-effort email join.
	if h.repo != nil {
		ids := map[string]bool{}
		for _, s := range rows {
			ids[s.AdminID] = true
		}
		emails := map[string]string{}
		for id := range ids {
			if a, _ := h.repo.FindByID(ctx, id); a != nil {
				emails[id] = a.Email
			}
		}
		for i := range rows {
			rows[i].AdminEmail = emails[rows[i].AdminID]
		}
	}

	response.WriteJSON(w, http.StatusOK, map[string]any{
		"sessions":   rows,
		"nextCursor": next,
	})
}

// GetSession handles GET /admin/v1/sessions/{id}.
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.sessionsRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session replay not wired"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/admin/v1/sessions/")
	if id == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing session id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	detail, err := h.sessionsRepo.GetSession(ctx, id)
	if err != nil {
		h.logger.Printf("admin /sessions/%s: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if detail == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if h.repo != nil {
		if a, _ := h.repo.FindByID(ctx, detail.Summary.AdminID); a != nil {
			detail.Summary.AdminEmail = a.Email
		}
	}

	response.WriteJSON(w, http.StatusOK, detail)
}
