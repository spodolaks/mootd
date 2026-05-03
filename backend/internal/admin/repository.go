package admin

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Repository is the persistence contract for admin records + refresh
// tokens + audit log. Implemented by MongoRepository for production; a
// minimal in-memory stub for tests can satisfy it without a live Mongo.
type Repository interface {
	// Admins
	FindByEmail(ctx context.Context, email string) (*Admin, error)
	FindByID(ctx context.Context, id string) (*Admin, error)
	Create(ctx context.Context, admin Admin) error
	UpdateLastActive(ctx context.Context, adminID string, at time.Time) error
	CountAdmins(ctx context.Context) (int64, error)

	// MFA — P5-02 / mootd-admin#35.
	// SetMFAEnrollment records secret + recovery hashes + flips
	// MFAEnforced=true atomically. Used by the verify endpoint.
	SetMFAEnrollment(ctx context.Context, adminID, secret string, recoveryHashes []string) error
	// SetMFARecoveryCodes replaces the recovery-code list (used
	// when ConsumeRecoveryCode burns one on login). Atomic.
	SetMFARecoveryCodes(ctx context.Context, adminID string, hashes []string) error
	// DisableMFA clears secret + recovery codes + flips
	// MFAEnforced=false. Reserved for "lost device" flows; not
	// exposed via HTTP in v1.
	DisableMFA(ctx context.Context, adminID string) error

	// Refresh tokens
	SaveRefreshToken(ctx context.Context, t RefreshToken) error
	FindRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string, at time.Time) error

	// Audit log (P0-04). Kept on the same Repository so handlers don't
	// need to thread a separate dependency; the storage is just one
	// extra collection in the same Mongo.
	AppendAudit(ctx context.Context, entry AuditEntry) error
	// ListAudit returns one page of entries matching q. Cursor
	// pagination on (at desc, _id desc).
	ListAudit(ctx context.Context, q AuditQuery) ([]AuditEntry, string, error)
}

// MongoRepository backs Repository with the production MongoDB cluster.
// Admins live in `admins`; refresh token history lives in
// `admin_refresh_tokens` (separate collection so the per-device issuance
// trail is preserved for audit, not overwritten on every login).
type MongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoRepository constructs a MongoRepository and ensures the
// expected indexes exist (idempotent — safe to call on every startup).
func NewMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*MongoRepository, error) {
	r := &MongoRepository{client: client, dbName: dbName}
	if err := r.ensureIndexes(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *MongoRepository) adminsCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("admins")
}

func (r *MongoRepository) tokensCol() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("admin_refresh_tokens")
}

// ensureIndexes declares the indexes the queries below rely on. Mongo
// is idempotent on CreateIndexes, so calling this at startup in every
// replica is safe.
func (r *MongoRepository) ensureIndexes(ctx context.Context) error {
	// admins: unique email
	_, err := r.adminsCol().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("admins_email_unique"),
	})
	if err != nil {
		return err
	}
	// admins: lookup disabled admins
	_, err = r.adminsCol().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "disabledAt", Value: 1}},
		Options: options.Index().SetName("admins_disabled_at").SetSparse(true),
	})
	if err != nil {
		return err
	}
	// admin_refresh_tokens: per-admin history, time-ordered
	_, err = r.tokensCol().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "adminId", Value: 1},
			{Key: "createdAt", Value: -1},
		},
		Options: options.Index().SetName("admin_refresh_tokens_admin_created"),
	})
	if err != nil {
		return err
	}
	// admin_refresh_tokens: automatic removal after expiry (mongo TTL)
	_, err = r.tokensCol().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expiresAt", Value: 1}},
		Options: options.Index().SetName("admin_refresh_tokens_ttl").SetExpireAfterSeconds(0),
	})
	if err != nil {
		return err
	}
	return r.ensureAuditIndexes(ctx)
}

// FindByEmail looks up an admin by lower-cased email. Returns (nil, nil)
// when no document matches so callers can treat "missing" and "found"
// symmetrically without error-type pattern matching.
func (r *MongoRepository) FindByEmail(ctx context.Context, email string) (*Admin, error) {
	var doc Admin
	err := r.adminsCol().FindOne(ctx, bson.M{"email": email}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// FindByID looks up an admin by _id.
func (r *MongoRepository) FindByID(ctx context.Context, id string) (*Admin, error) {
	var doc Admin
	err := r.adminsCol().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// Create inserts a new admin. Expected to bubble up the duplicate-key
// error from the unique email index so callers (the bootstrap command)
// can distinguish "already exists" from a real failure.
func (r *MongoRepository) Create(ctx context.Context, admin Admin) error {
	_, err := r.adminsCol().InsertOne(ctx, admin)
	return err
}

// UpdateLastActive bumps the lastActiveAt timestamp on an admin doc.
// Best-effort from login / refresh — a failure logs but doesn't block.
func (r *MongoRepository) UpdateLastActive(ctx context.Context, adminID string, at time.Time) error {
	_, err := r.adminsCol().UpdateOne(ctx, bson.M{"_id": adminID}, bson.M{
		"$set": bson.M{"lastActiveAt": at},
	})
	return err
}

// CountAdmins reports how many admin documents exist. Used by the
// bootstrap command to refuse re-bootstrap (so a misconfigured prod
// restart can't accidentally replace the first admin).
func (r *MongoRepository) CountAdmins(ctx context.Context) (int64, error) {
	return r.adminsCol().CountDocuments(ctx, bson.M{})
}

// SetMFAEnrollment is the atomic verify-and-enable step
// (P5-02 / mootd-admin#35). Stores secret + hashed recovery
// codes + flips MFAEnforced=true in one update so a partial
// failure can't leave the admin in a "secret saved but not
// enforced" half-state.
func (r *MongoRepository) SetMFAEnrollment(ctx context.Context, adminID, secret string, recoveryHashes []string) error {
	if adminID == "" || secret == "" {
		return errors.New("admin: SetMFAEnrollment requires id + secret")
	}
	_, err := r.adminsCol().UpdateOne(ctx, bson.M{"_id": adminID}, bson.M{
		"$set": bson.M{
			"mfaSecret":        secret,
			"mfaRecoveryCodes": recoveryHashes,
			"mfaEnforced":      true,
			"updatedAt":        time.Now().UTC(),
		},
	})
	return err
}

// SetMFARecoveryCodes replaces the recovery-code list — used
// after ConsumeRecoveryCode burns a code on login. Atomic.
func (r *MongoRepository) SetMFARecoveryCodes(ctx context.Context, adminID string, hashes []string) error {
	_, err := r.adminsCol().UpdateOne(ctx, bson.M{"_id": adminID}, bson.M{
		"$set": bson.M{"mfaRecoveryCodes": hashes},
	})
	return err
}

// DisableMFA clears MFA state. Not exposed via HTTP today —
// reserved for the "lost device" emergency procedure documented
// in the runbook. Operator runs this directly against the DB
// after verifying identity out-of-band.
func (r *MongoRepository) DisableMFA(ctx context.Context, adminID string) error {
	_, err := r.adminsCol().UpdateOne(ctx, bson.M{"_id": adminID}, bson.M{
		"$set": bson.M{"mfaEnforced": false},
		"$unset": bson.M{
			"mfaSecret":        "",
			"mfaRecoveryCodes": "",
		},
	})
	return err
}

// SaveRefreshToken inserts a new refresh token record. The _id is the
// sha256 of the raw token (computed by the caller via jwt.HashRefreshToken).
func (r *MongoRepository) SaveRefreshToken(ctx context.Context, t RefreshToken) error {
	_, err := r.tokensCol().InsertOne(ctx, t)
	return err
}

// FindRefreshToken returns the record matching tokenHash — but only if
// it's still valid (not expired, not revoked). Returns (nil, nil) for
// any form of invalid match so callers can treat "rejected" uniformly.
func (r *MongoRepository) FindRefreshToken(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var doc RefreshToken
	err := r.tokensCol().FindOne(ctx, bson.M{
		"_id":       tokenHash,
		"expiresAt": bson.M{"$gt": time.Now().UTC()},
		"revokedAt": bson.M{"$exists": false},
	}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// RevokeRefreshToken marks a refresh token as revoked. Used when the
// token is rotated on refresh (single-use tokens — the old one dies the
// moment the new pair is issued).
func (r *MongoRepository) RevokeRefreshToken(ctx context.Context, tokenHash string, at time.Time) error {
	_, err := r.tokensCol().UpdateOne(ctx, bson.M{"_id": tokenHash}, bson.M{
		"$set": bson.M{"revokedAt": at},
	})
	return err
}
