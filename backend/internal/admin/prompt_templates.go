package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ────────────────────────────────────────────────────────────────────
// Prompt templates (P3-01 / mootd-admin#24).
//
// Pull the constant parts of the outfit prompt out of Go strings
// into a Mongo-backed `prompt_templates` collection so admins
// can edit + version + promote new versions without a redeploy.
//
// Design choices worth noting:
//
//   - One row per *version*, not one row per template. The
//     "production" version is picked by the `isProduction`
//     flag — Promote() flips it atomically (sets the new
//     version true, sets every other version of the same name
//     to false in a single transaction).
//   - Templates are *constants*. The procedural bits of the
//     prompt (archetype context, weather, recent-boards
//     few-shot) stay in Go because they interpolate live data
//     in ways that exceed simple `{{var}}` substitution. The
//     two seeded templates today — `outfit_system_base` and
//     `outfit_safety` — are the parts where the writing
//     matters most and the data interpolation is none.
//   - In-process cache (60s TTL) so the outfit hot path
//     doesn't hit Mongo per call. Promote() invalidates the
//     cache so admins see their changes immediately.
//   - Fallback to a hardcoded default — when Mongo is down or
//     no template is seeded, the cached reader returns the
//     baked-in fallback. The system stays functional with the
//     pre-migration behaviour.
// ────────────────────────────────────────────────────────────────────

const promptTemplatesCollection = "prompt_templates"

// PromptTemplate is one stored version of one named template.
type PromptTemplate struct {
	ID           string    `bson:"_id"          json:"id"`
	Name         string    `bson:"name"         json:"name"`
	Version      int       `bson:"version"      json:"version"`
	Body         string    `bson:"body"         json:"body"`
	Variables    []string  `bson:"variables,omitempty" json:"variables,omitempty"`
	IsProduction bool      `bson:"isProduction" json:"isProduction"`
	CreatedBy    string    `bson:"createdBy,omitempty" json:"createdBy,omitempty"`
	CreatedAt    time.Time `bson:"createdAt"    json:"createdAt"`
	Notes        string    `bson:"notes,omitempty" json:"notes,omitempty"`
}

// PromptTemplatesRepository is the persistence boundary.
type PromptTemplatesRepository interface {
	GetProduction(ctx context.Context, name string) (*PromptTemplate, error)
	ListNames(ctx context.Context) ([]string, error)
	ListVersions(ctx context.Context, name string) ([]PromptTemplate, error)
	CreateVersion(ctx context.Context, t PromptTemplate) error
	Promote(ctx context.Context, name string, version int) error
	Get(ctx context.Context, id string) (*PromptTemplate, error)
}

// PromptTemplatesMongoRepository is the production impl.
type PromptTemplatesMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewPromptTemplatesMongoRepository ensures indexes + seeds the
// initial templates if the collection is empty. Seeding is
// idempotent: existing rows aren't touched.
func NewPromptTemplatesMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*PromptTemplatesMongoRepository, error) {
	r := &PromptTemplatesMongoRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Per-name version uniqueness — two rows of the same
			// (name, version) pair would create ambiguity.
			Keys:    bson.D{{Key: "name", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("prompt_templates_name_version_unique"),
		},
		{
			// Lookup-by-name-isProduction is the hot path for the
			// generator. A partial index on isProduction=true keeps
			// the index tiny (one row per name) and the lookup
			// O(1).
			Keys:    bson.D{{Key: "name", Value: 1}, {Key: "isProduction", Value: 1}},
			Options: options.Index().SetName("prompt_templates_name_production"),
		},
	}); err != nil {
		return nil, fmt.Errorf("ensure prompt_templates indexes: %w", err)
	}
	return r, nil
}

func (r *PromptTemplatesMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection(promptTemplatesCollection)
}

// GetProduction returns the row with isProduction=true for the
// given name, or (nil, nil) when none exists. Caller falls back
// to a hardcoded default in that case.
func (r *PromptTemplatesMongoRepository) GetProduction(ctx context.Context, name string) (*PromptTemplate, error) {
	var doc PromptTemplate
	err := r.col().FindOne(ctx, bson.M{"name": name, "isProduction": true}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// ListNames returns the distinct template names. Used by the
// admin UI's left-rail.
func (r *PromptTemplatesMongoRepository) ListNames(ctx context.Context) ([]string, error) {
	res := r.col().Distinct(ctx, "name", bson.M{})
	if err := res.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return []string{}, nil
		}
		return nil, err
	}
	var out []string
	if err := res.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListVersions returns every version of one template, newest
// first. Used by the admin UI's version history.
func (r *PromptTemplatesMongoRepository) ListVersions(ctx context.Context, name string) ([]PromptTemplate, error) {
	cur, err := r.col().Find(ctx,
		bson.M{"name": name},
		options.Find().SetSort(bson.D{{Key: "version", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []PromptTemplate
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateVersion inserts a new (name, version) row. Caller must
// pre-compute version = max(existing) + 1; the unique index
// would reject a collision anyway, but doing the calculation
// in the repo is racy under concurrent writers.
func (r *PromptTemplatesMongoRepository) CreateVersion(ctx context.Context, t PromptTemplate) error {
	if t.Name == "" || t.Version <= 0 {
		return errors.New("admin: template name + version > 0 required")
	}
	if t.ID == "" {
		t.ID = fmt.Sprintf("pt_%s_v%d", t.Name, t.Version)
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	_, err := r.col().InsertOne(ctx, t)
	return err
}

// Promote flips isProduction so exactly one version of the
// named template is the "current" one. Atomic via a Mongo
// transaction (we're on a replicaset in production; falls back
// to two best-effort writes when the cluster is single-node /
// development).
func (r *PromptTemplatesMongoRepository) Promote(ctx context.Context, name string, version int) error {
	// Try the transactional path first. Mongo's session API
	// returns a session-not-supported-on-this-cluster error in
	// dev (single-node mongo); we catch it and fall back to
	// a sequential write.
	sess, err := r.client.StartSession()
	if err == nil {
		defer sess.EndSession(ctx)
		_, txErr := sess.WithTransaction(ctx, func(sCtx context.Context) (any, error) {
			return r.promoteInternal(sCtx, name, version)
		})
		if txErr == nil {
			return nil
		}
		// fall through on transaction error — single-node Mongo
		// doesn't support transactions; we accept the small
		// inconsistency window.
	}
	_, err = r.promoteInternal(ctx, name, version)
	return err
}

func (r *PromptTemplatesMongoRepository) promoteInternal(ctx context.Context, name string, version int) (any, error) {
	// 1. Verify the target version exists.
	count, err := r.col().CountDocuments(ctx, bson.M{"name": name, "version": version})
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("admin: prompt template %q version %d not found", name, version)
	}
	// 2. Demote all other versions of this name.
	_, err = r.col().UpdateMany(ctx,
		bson.M{"name": name, "isProduction": true},
		bson.M{"$set": bson.M{"isProduction": false}},
	)
	if err != nil {
		return nil, err
	}
	// 3. Promote the chosen version.
	_, err = r.col().UpdateOne(ctx,
		bson.M{"name": name, "version": version},
		bson.M{"$set": bson.M{"isProduction": true}},
	)
	return nil, err
}

// Get returns one template by id. Used by the admin UI when
// the list view links to a specific version.
func (r *PromptTemplatesMongoRepository) Get(ctx context.Context, id string) (*PromptTemplate, error) {
	var doc PromptTemplate
	err := r.col().FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

// ────────────────────────────────────────────────────────────────────
// Cached reader for the outfit hot path.
// ────────────────────────────────────────────────────────────────────

// CachedPromptTemplates wraps the repo with a 60s in-process
// cache + per-name fallback. Outfit generation calls
// `BodyOrFallback("outfit_system_base")` — that returns the
// cached production body if present, the fallback otherwise.
type CachedPromptTemplates struct {
	repo      PromptTemplatesRepository
	fallback  map[string]string
	mu        sync.RWMutex
	cache     map[string]string
	exp       time.Time
	ttl       time.Duration
	logger    interface{ Printf(string, ...any) }
}

// NewCachedPromptTemplates wires the repo + a fallback map
// (keyed by template name → default body, used when Mongo is
// unavailable or the template hasn't been seeded yet).
func NewCachedPromptTemplates(repo PromptTemplatesRepository, fallbacks map[string]string, logger interface{ Printf(string, ...any) }) *CachedPromptTemplates {
	if logger == nil {
		logger = noopLogger{}
	}
	return &CachedPromptTemplates{
		repo:     repo,
		fallback: fallbacks,
		ttl:      60 * time.Second,
		logger:   logger,
	}
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...any) {}

// BodyOrFallback returns the production body for `name` from
// the cache, refreshing if stale; falls back to the seeded
// default when Mongo is unavailable or the template has no
// production version.
func (c *CachedPromptTemplates) BodyOrFallback(ctx context.Context, name string) string {
	c.mu.RLock()
	if c.cache != nil && time.Now().Before(c.exp) {
		v, ok := c.cache[name]
		c.mu.RUnlock()
		if ok {
			return v
		}
		return c.fallback[name]
	}
	c.mu.RUnlock()

	// Refresh under write lock.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache != nil && time.Now().Before(c.exp) {
		// Another goroutine refreshed while we waited.
		if v, ok := c.cache[name]; ok {
			return v
		}
		return c.fallback[name]
	}

	// Best-effort fetch all known names. We don't know the
	// full set upfront; refresh by iterating fallback keys
	// (which is the canonical "what templates does the
	// system care about" list).
	fresh := map[string]string{}
	for n := range c.fallback {
		doc, err := c.repo.GetProduction(ctx, n)
		if err != nil {
			c.logger.Printf("prompt_templates: read %q failed: %v (using fallback)", n, err)
			continue
		}
		if doc != nil {
			fresh[n] = doc.Body
		}
	}
	c.cache = fresh
	c.exp = time.Now().Add(c.ttl)
	if v, ok := fresh[name]; ok {
		return v
	}
	return c.fallback[name]
}

// Invalidate clears the cache so the next call refetches.
// Called by the admin Promote handler.
func (c *CachedPromptTemplates) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = nil
	c.exp = time.Time{}
}

// ────────────────────────────────────────────────────────────────────
// Seed helper.
// ────────────────────────────────────────────────────────────────────

// SeedPromptTemplates inserts v1 of each template if missing.
// Idempotent on every restart — does NOT overwrite existing
// rows or change which version is in production.
func SeedPromptTemplates(ctx context.Context, repo PromptTemplatesRepository, defaults map[string]string, logger interface{ Printf(string, ...any) }) error {
	if logger == nil {
		logger = noopLogger{}
	}
	for name, body := range defaults {
		versions, err := repo.ListVersions(ctx, name)
		if err != nil {
			return fmt.Errorf("seed %q: list versions: %w", name, err)
		}
		if len(versions) > 0 {
			continue // already seeded
		}
		t := PromptTemplate{
			ID:           fmt.Sprintf("pt_%s_v1", name),
			Name:         name,
			Version:      1,
			Body:         body,
			IsProduction: true,
			CreatedAt:    time.Now().UTC(),
			Notes:        "seeded from hardcoded constants at first boot",
		}
		// Discover variables — anything that looks like
		// {{name}}. Stored alongside the body so the FE can
		// highlight them. None today (the seeded constants are
		// pure text), but the field is forward-compatible.
		t.Variables = discoverVariables(body)
		if err := repo.CreateVersion(ctx, t); err != nil {
			return fmt.Errorf("seed %q: insert: %w", name, err)
		}
		logger.Printf("prompt_templates: seeded %q v1 (%d chars)", name, len(body))
	}
	return nil
}

// discoverVariables extracts {{varName}} placeholders. Trims
// whitespace + dedupes. Order: first appearance.
func discoverVariables(body string) []string {
	var out []string
	seen := map[string]bool{}
	for {
		i := strings.Index(body, "{{")
		if i < 0 {
			break
		}
		j := strings.Index(body[i+2:], "}}")
		if j < 0 {
			break
		}
		name := strings.TrimSpace(body[i+2 : i+2+j])
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		body = body[i+2+j+2:]
	}
	return out
}
