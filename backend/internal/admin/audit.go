package admin

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// AuditEntry is one row in the admin_audit collection. Append-only —
// any update or delete is treated as a bug. We never return entries
// from a public endpoint without an admin reading them; audit is
// itself a privileged surface (P5).
//
// Fields denormalised on purpose: AdminEmail is captured at write time
// so a renamed admin doesn't rewrite history; AdminID is the join key
// for new-stage analytics.
type AuditEntry struct {
	ID           string         `bson:"_id" json:"id"`
	AdminID      string         `bson:"adminId" json:"adminId"`
	AdminEmail   string         `bson:"adminEmail" json:"adminEmail,omitempty"`
	Action       string         `bson:"action" json:"action"`
	TargetUserID string         `bson:"targetUserId,omitempty" json:"targetUserId,omitempty"`
	TargetEntity string         `bson:"targetEntity,omitempty" json:"targetEntity,omitempty"`
	Metadata     map[string]any `bson:"metadata,omitempty" json:"metadata,omitempty"`
	At           time.Time      `bson:"at" json:"at"`
	IP           string         `bson:"ip,omitempty" json:"ip,omitempty"`
	UserAgent    string         `bson:"userAgent,omitempty" json:"userAgent,omitempty"`
}

// AuditRepository is the persistence contract for the audit log.
type AuditRepository interface {
	AppendAudit(ctx context.Context, entry AuditEntry) error
	// ListAudit returns one page of entries matching q, plus the
	// cursor for the next page (empty when no more). Cursor encodes
	// the last entry's _id; sort is always (at desc, _id desc).
	ListAudit(ctx context.Context, q AuditQuery) ([]AuditEntry, string, error)
}

// AuditQuery is the filter set accepted by GET /admin/v1/audit.
// All fields optional — the empty query returns the most-recent
// page across the whole log.
type AuditQuery struct {
	Action       string     // exact match (e.g. "traces.export")
	AdminID      string     // exact match
	TargetUserID string     // exact match
	From         *time.Time // at >= From
	To           *time.Time // at < To (exclusive)
	Cursor       string     // last entry's _id from previous page
	Limit        int        // 1..100; default 25
}

// AuditPage is the wire shape returned to the admin UI.
type AuditPage struct {
	Entries    []AuditEntry `json:"entries"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// Implementations on MongoRepository ──────────────────────────────────

func (r *MongoRepository) auditCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("admin_audit")
}

// AppendAudit inserts one audit row. Errors are surfaced to the caller
// so a missing audit row can be flagged in monitoring; callers that
// don't want to fail the whole request on audit failure should log
// the error and continue (a write that succeeded but isn't audited is
// a worse outcome than a write that succeeded and the audit log shows
// it but the caller saw 500).
func (r *MongoRepository) AppendAudit(ctx context.Context, entry AuditEntry) error {
	_, err := r.auditCol().InsertOne(ctx, entry)
	return err
}

// ListAudit returns one page of audit entries matching q. Sort is
// always (at desc, _id desc) so cursor pagination is stable when many
// rows share the same millisecond. Rides the existing
// (action, at), (adminId, at), (targetUserId, at) indexes — single
// indexed scan per filter combination.
func (r *MongoRepository) ListAudit(ctx context.Context, q AuditQuery) ([]AuditEntry, string, error) {
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	filter := bson.M{}
	if q.Action != "" {
		filter["action"] = q.Action
	}
	if q.AdminID != "" {
		filter["adminId"] = q.AdminID
	}
	if q.TargetUserID != "" {
		filter["targetUserId"] = q.TargetUserID
	}
	if q.From != nil || q.To != nil {
		ts := bson.M{}
		if q.From != nil {
			ts["$gte"] = q.From.UTC()
		}
		if q.To != nil {
			ts["$lt"] = q.To.UTC()
		}
		filter["at"] = ts
	}
	if q.Cursor != "" {
		// Cursor is the previous page's last _id. With (at desc, _id
		// desc) order, "older than that one" means "_id < cursor".
		filter["_id"] = bson.M{"$lt": q.Cursor}
	}

	cur, err := r.auditCol().Find(ctx, filter,
		findOpts().
			SetSort(bson.D{{Key: "at", Value: -1}, {Key: "_id", Value: -1}}).
			SetLimit(int64(limit+1))) // +1 to detect more
	if err != nil {
		return nil, "", err
	}
	defer cur.Close(ctx)

	var entries []AuditEntry
	if err := cur.All(ctx, &entries); err != nil {
		return nil, "", err
	}

	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	nextCursor := ""
	if hasMore && len(entries) > 0 {
		nextCursor = entries[len(entries)-1].ID
	}
	return entries, nextCursor, nil
}

// ensureAuditIndexes is called from ensureIndexes. Kept separate so
// the index list reads naturally per-collection.
func (r *MongoRepository) ensureAuditIndexes(ctx context.Context) error {
	_, err := r.auditCol().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Per-admin chronological lookups.
			Keys: bson.D{{Key: "adminId", Value: 1}, {Key: "at", Value: -1}},
		},
		{
			// Per-target lookups ("show me everything ever done to user X").
			Keys: bson.D{{Key: "targetUserId", Value: 1}, {Key: "at", Value: -1}},
		},
		{
			// Filter by action across all admins.
			Keys: bson.D{{Key: "action", Value: 1}, {Key: "at", Value: -1}},
		},
	})
	return err
}

// Audit is a fire-and-forget helper for handlers. Failures are logged
// but never surfaced — losing an audit row is bad but not as bad as
// blocking the user's action. P5-05 monitoring will alert on audit
// write rate dropping unexpectedly.
func Audit(ctx context.Context, repo AuditRepository, logger *log.Logger, entry AuditEntry) {
	if err := repo.AppendAudit(ctx, entry); err != nil {
		logger.Printf("admin audit: append failed: %v (entry=%+v)", err, entry)
	}
}

// generateAuditID produces a unique ID for a new audit row. ULIDs would
// give natural time ordering; the existing shared/id package emits
// hex-prefixed IDs. We reuse that style for consistency with the rest
// of the backend's _id values.
func generateAuditID() string {
	return "aud_" + randomHex(16)
}
