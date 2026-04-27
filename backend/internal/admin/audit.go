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
	ID            string         `bson:"_id"`
	AdminID       string         `bson:"adminId"`
	AdminEmail    string         `bson:"adminEmail"`
	Action        string         `bson:"action"`
	TargetUserID  string         `bson:"targetUserId,omitempty"`
	TargetEntity  string         `bson:"targetEntity,omitempty"`
	Metadata      map[string]any `bson:"metadata,omitempty"`
	At            time.Time      `bson:"at"`
	IP            string         `bson:"ip,omitempty"`
	UserAgent     string         `bson:"userAgent,omitempty"`
}

// AuditRepository is the persistence contract for the audit log.
// Kept narrow on purpose — Phase 0 only needs Append; reads land in
// P5-04 when the admin UI exposes the log.
type AuditRepository interface {
	AppendAudit(ctx context.Context, entry AuditEntry) error
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
