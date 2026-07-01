package admin

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ────────────────────────────────────────────────────────────────────
// Prompt A/B testing (P3-05 / mootd-admin#28).
//
// Singleton-per-template model: at any given time a template
// either has *no* active test or *exactly one*. Starting a test
// while one is active rejects with 409. This keeps the routing
// decision dead simple at call time:
//
//   1. Look up active test for this template (cached, 60s TTL).
//   2. If none: serve the production version. (Pre-#28 behaviour.)
//   3. Hash the user's id → percentage. If in [0, trafficPct):
//      serve the candidate version. Else serve production.
//
// The split is **deterministic by userId** — same user always
// hits the same arm, even across server restarts. That's the
// "consistent experience" property the issue calls out.
//
// Stats are computable retrospectively from the existing
// llm_calls.promptVersion field (already populated since
// mootd@P1-01). Filter by promptVersion = candidate vs
// production for the active test window and compare in the
// /traces page. Auto-promote on win is deferred.
// ────────────────────────────────────────────────────────────────────

const promptABTestsCollection = "prompt_ab_tests"

// ABTestStatus is the lifecycle.
type ABTestStatus string

const (
	ABTestActive ABTestStatus = "active"
	ABTestEnded  ABTestStatus = "ended"
)

// ABTest is one stored row.
type ABTest struct {
	ID                string       `bson:"_id"                json:"id"`
	TemplateName      string       `bson:"templateName"       json:"templateName"`
	ProductionVersion int          `bson:"productionVersion"  json:"productionVersion"`
	CandidateVersion  int          `bson:"candidateVersion"   json:"candidateVersion"`
	TrafficPct        int          `bson:"trafficPct"         json:"trafficPct"` // 0-100
	Status            ABTestStatus `bson:"status"             json:"status"`
	StartedBy         string       `bson:"startedBy,omitempty" json:"startedBy,omitempty"`
	StartedAt         time.Time    `bson:"startedAt"          json:"startedAt"`
	EndedAt           *time.Time   `bson:"endedAt,omitempty"  json:"endedAt,omitempty"`
	EndedBy           string       `bson:"endedBy,omitempty"  json:"endedBy,omitempty"`
	Notes             string       `bson:"notes,omitempty"    json:"notes,omitempty"`
}

// ABTestRepository is the persistence boundary.
type ABTestRepository interface {
	Active(ctx context.Context, templateName string) (*ABTest, error)
	List(ctx context.Context, templateName string) ([]ABTest, error)
	Start(ctx context.Context, t ABTest) error
	End(ctx context.Context, id string, endedBy string, notes string) error
}

// ABTestMongoRepository implements ABTestRepository.
type ABTestMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewABTestMongoRepository ensures indexes. Partial unique
// index on (templateName) where status=active enforces "at
// most one active test per template" at the storage layer —
// concurrent Start() calls can't both win.
func NewABTestMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*ABTestMongoRepository, error) {
	r := &ABTestMongoRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "templateName", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetPartialFilterExpression(bson.M{"status": ABTestActive}).
				SetName("prompt_ab_tests_one_active_per_template"),
		},
		{
			// History queries.
			Keys:    bson.D{{Key: "templateName", Value: 1}, {Key: "startedAt", Value: -1}},
			Options: options.Index().SetName("prompt_ab_tests_template_started"),
		},
	}); err != nil {
		return nil, fmt.Errorf("ensure prompt_ab_tests indexes: %w", err)
	}
	return r, nil
}

func (r *ABTestMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection(promptABTestsCollection)
}

func (r *ABTestMongoRepository) Active(ctx context.Context, templateName string) (*ABTest, error) {
	var doc ABTest
	err := r.col().FindOne(ctx, bson.M{
		"templateName": templateName,
		"status":       ABTestActive,
	}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (r *ABTestMongoRepository) List(ctx context.Context, templateName string) ([]ABTest, error) {
	cur, err := r.col().Find(ctx,
		bson.M{"templateName": templateName},
		options.Find().SetSort(bson.D{{Key: "startedAt", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []ABTest
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ABTestMongoRepository) Start(ctx context.Context, t ABTest) error {
	if t.TemplateName == "" {
		return errors.New("admin: ab test templateName required")
	}
	if t.TrafficPct < 1 || t.TrafficPct > 99 {
		return errors.New("admin: trafficPct must be 1-99 (50 = balanced split)")
	}
	if t.ID == "" {
		t.ID = "ab_" + generateAuditID()[len("aud_"):]
	}
	if t.StartedAt.IsZero() {
		t.StartedAt = time.Now().UTC()
	}
	t.Status = ABTestActive
	_, err := r.col().InsertOne(ctx, t)
	if mongo.IsDuplicateKeyError(err) {
		return errors.New("admin: a test is already active on this template; end it first")
	}
	return err
}

func (r *ABTestMongoRepository) End(ctx context.Context, id, endedBy, notes string) error {
	now := time.Now().UTC()
	res, err := r.col().UpdateOne(ctx,
		bson.M{"_id": id, "status": ABTestActive},
		bson.M{"$set": bson.M{
			"status":  ABTestEnded,
			"endedAt": now,
			"endedBy": endedBy,
			"notes":   notes,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errors.New("admin: no active test with that id")
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────
// Cached active-test reader.
// ────────────────────────────────────────────────────────────────────

// CachedABTests wraps the repo with a 60s in-process cache,
// invalidated on Start/End so admins see their changes
// immediately. Read shape: ActiveForTemplate(name) -> *ABTest.
type CachedABTests struct {
	repo ABTestRepository
	mu   sync.RWMutex
	// active maps templateName → *ABTest (nil for "no test").
	// nil entries are real cache hits (we know there's no test);
	// missing keys mean "we haven't checked this template yet."
	active map[string]*ABTest
	exp    time.Time
	ttl    time.Duration
}

// NewCachedABTests wires the cache. ttl=0 picks 60s.
func NewCachedABTests(repo ABTestRepository, ttl time.Duration) *CachedABTests {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &CachedABTests{repo: repo, ttl: ttl}
}

// ActiveForTemplate returns the active test for `name`, or nil
// when none. Errors return (nil, nil) — A/B routing is
// best-effort and a Mongo blip should never block production
// traffic; we just don't route to the candidate that one call.
func (c *CachedABTests) ActiveForTemplate(ctx context.Context, name string) *ABTest {
	if c == nil || c.repo == nil {
		return nil
	}

	c.mu.RLock()
	if time.Now().Before(c.exp) && c.active != nil {
		t := c.active[name]
		c.mu.RUnlock()
		return t
	}
	c.mu.RUnlock()

	// Refresh under write lock.
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.exp) && c.active != nil {
		return c.active[name]
	}
	t, err := c.repo.Active(ctx, name)
	if err != nil {
		// Stale-cache-or-bust: keep whatever we had, swallow
		// the error. Safer than a brief "no test" blip.
		return nil
	}
	if c.active == nil {
		c.active = map[string]*ABTest{}
	}
	c.active[name] = t
	c.exp = time.Now().Add(c.ttl)
	return t
}

// Invalidate clears the cache so the next read refetches.
func (c *CachedABTests) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active = nil
	c.exp = time.Time{}
}

// ────────────────────────────────────────────────────────────────────
// User-ID hash → percentage bucket.
// ────────────────────────────────────────────────────────────────────

// UserBucketPct returns a number in [0, 100) deterministically
// derived from `userID + templateName`. Including the template
// name in the hash means a single user is independently
// bucketed across simultaneous tests on different templates —
// they're not always in the "lucky" or "unlucky" half across
// every prompt.
//
// SHA-256 takes 8 bytes as a uint64 mod 100. Excellent
// distribution; over-engineered for the precision we need but
// the cost is negligible (one hash per generation call) and
// the reproducibility from the well-known algo is its own
// value.
func UserBucketPct(userID, templateName string) int {
	if userID == "" {
		// Anonymous / system calls have no stable identity to hash.
		// The always-serve-production rule for them lives in
		// IsCandidateUser — a bucket value can't express it, because
		// 0 still satisfies `bucket < TrafficPct` for every valid
		// TrafficPct (1-99). See #156: relying on bucket 0 here
		// routed all anonymous/eval traffic to the candidate arm.
		return 0
	}
	h := sha256.Sum256([]byte(userID + ":" + templateName))
	v := binary.BigEndian.Uint64(h[:8])
	return int(v % 100)
}

// IsCandidateUser is the per-call decision: true → serve
// candidate, false → serve production.
//
// Anonymous / system / eval-harness calls (empty userID) always
// serve production: routing them to the candidate would skew the
// test arm with non-comparable traffic and make eval scores
// incomparable across runs (outfit/prompts.go relies on this for
// BuildSystemPromptForEval). Guarded here on the decision, not in
// UserBucketPct, because bucket 0 is still < every valid
// TrafficPct (#156).
func IsCandidateUser(userID string, t *ABTest) bool {
	if t == nil || t.Status != ABTestActive {
		return false
	}
	if userID == "" {
		return false
	}
	return UserBucketPct(userID, t.TemplateName) < t.TrafficPct
}
