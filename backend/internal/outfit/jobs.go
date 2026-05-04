package outfit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// JobStatus represents the state of an async outfit generation job.
type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobProcessing JobStatus = "processing"
	JobCompleted  JobStatus = "completed"
	JobFailed     JobStatus = "failed"
)

// Job represents an async outfit generation job.
//
// Stored both in Mongo (durable, survives restart) and Redis (fast
// reads while the user polls). Mongo is the source of truth; Redis
// is a write-through cache. See JobStore for the dual-write logic.
type Job struct {
	ID        string    `json:"id"        bson:"_id"`
	UserID    string    `json:"userID"    bson:"userId"`
	Status    JobStatus `json:"status"    bson:"status"`
	Outfits   []Outfit  `json:"outfits,omitempty" bson:"outfits,omitempty"`
	Error     string    `json:"error,omitempty"   bson:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt,omitempty" bson:"updatedAt,omitempty"`
}

// jobMongoTTL is how long completed/failed rows linger in Mongo
// before the TTL index reaps them. 24h gives an admin audit window
// without unbounded growth. Pending/processing rows never get
// reaped — recoverStaleJobs handles those.
const jobMongoTTL = 24 * time.Hour

// jobStaleProcessing is the cutoff after which a `processing` job
// is treated as orphaned (server crashed mid-generation). The
// outfit pipeline normally completes well under 5 minutes; anything
// older than that with `processing` status was almost certainly
// abandoned by a dead goroutine.
const jobStaleProcessing = 10 * time.Minute

// JobStore manages async outfit generation jobs.
//
// Dual storage:
//
//   - Mongo (`outfit_jobs`): durable; survives backend restart so a
//     completed job's results aren't lost when the user comes back.
//     TTL index reaps rows after jobMongoTTL.
//   - Redis (`outfit:job:{id}`): fast-path cache. Writes mirror Mongo;
//     reads check Redis first and fall back to Mongo on miss
//     (common after Redis flush or restart). Failures are logged
//     but never block the user-facing flow.
//
// Redis is optional — caller passes nil when Redis is unavailable;
// the store still works, just with one fewer cache layer.
type JobStore struct {
	mongoCol *mongo.Collection
	redis    *redis.Client
	prefix   string
	redisTTL time.Duration
	logger   *log.Logger
}

// NewJobStore constructs the store and ensures the Mongo TTL index
// exists. The Mongo collection is the source of truth; redisClient
// may be nil — when nil, the cache layer is skipped silently.
func NewJobStore(ctx context.Context, mongoClient *mongo.Client, dbName string, redisClient *redis.Client, logger *log.Logger) (*JobStore, error) {
	if mongoClient == nil {
		return nil, errors.New("outfit: JobStore requires a Mongo client")
	}
	if logger == nil {
		logger = log.Default()
	}
	col := mongoClient.Database(dbName).Collection("outfit_jobs")
	// TTL on createdAt — Mongo's reaper sweeps once a minute. Set
	// at TTL+jitter to avoid a thundering herd of expirations.
	if _, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "createdAt", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(int32(jobMongoTTL / time.Second)).SetName("outfit_jobs_ttl"),
	}); err != nil {
		return nil, fmt.Errorf("ensure outfit_jobs TTL index: %w", err)
	}
	// Per-user lookup for the future "show me my recent jobs" feature.
	if _, err := col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}},
		Options: options.Index().SetName("outfit_jobs_user_created"),
	}); err != nil {
		return nil, fmt.Errorf("ensure outfit_jobs user index: %w", err)
	}
	// Recover any jobs left in `processing` from a previous boot —
	// goroutines that would have flipped them to completed/failed
	// died with the old process. Mark them failed so the FE shows a
	// retry instead of polling forever.
	if err := recoverStaleJobs(ctx, col, logger); err != nil {
		// Non-fatal: stale jobs just keep their state, FE polls
		// continue (and time out client-side). Log + carry on.
		logger.Printf("outfit: stale-job recovery: %v", err)
	}

	return &JobStore{
		mongoCol: col,
		redis:    redisClient,
		prefix:   "outfit:job:",
		redisTTL: 10 * time.Minute,
		logger:   logger,
	}, nil
}

// Save writes the job to Mongo (durable) and Redis (cache, when
// available). Mongo failure is fatal — the caller needs to know the
// job state didn't persist. Redis failure logs and continues — the
// next read just falls through to Mongo.
func (s *JobStore) Save(ctx context.Context, job *Job) error {
	job.UpdatedAt = time.Now().UTC()

	// Mongo first — source of truth. Replace-on-id so the
	// pending→processing→completed transitions overwrite cleanly.
	if _, err := s.mongoCol.ReplaceOne(ctx, bson.M{"_id": job.ID}, job, options.Replace().SetUpsert(true)); err != nil {
		return fmt.Errorf("outfit: mongo save: %w", err)
	}

	// Redis: best-effort write-through cache.
	if s.redis != nil {
		data, err := json.Marshal(job)
		if err != nil {
			s.logger.Printf("outfit: marshal job %s for redis: %v", job.ID, err)
			return nil
		}
		if err := s.redis.Set(ctx, s.prefix+job.ID, data, s.redisTTL).Err(); err != nil {
			s.logger.Printf("outfit: redis cache save for job %s: %v (continuing)", job.ID, err)
		}
	}
	return nil
}

// idempotencyKeyTTL is how long an Idempotency-Key → jobID
// mapping survives in Redis (mootd#42). 60 seconds covers the
// "user double-tapped + RN client retried on flaky network"
// window. Generation usually completes in 5–30s, so by the
// time the TTL elapses the user has either seen the job
// resolve or moved on.
const idempotencyKeyTTL = 60 * time.Second

// idempotencyPrefix namespaces the Redis keys so a stray flush
// of `outfit:idem:*` doesn't collide with `outfit:job:*`.
const idempotencyPrefix = "outfit:idem:"

// LookupIdempotency returns the jobID a previous request bound
// to this (userID, key) pair, if any. Empty string + nil error
// when no mapping exists. Errors only on infrastructure failure
// — a Redis miss is treated as "no mapping, proceed".
func (s *JobStore) LookupIdempotency(ctx context.Context, userID, key string) (string, error) {
	if s.redis == nil || key == "" || userID == "" {
		return "", nil
	}
	jobID, err := s.redis.Get(ctx, idempotencyKey(userID, key)).Result()
	if err == nil {
		return jobID, nil
	}
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return "", err
}

// SaveIdempotency binds (userID, key) → jobID for the
// idempotencyKeyTTL window. Caller-side semantics: a second
// SubmitGenerate with the same key inside the window returns
// the same jobID instead of starting a new job.
//
// Best-effort: failures log inside the caller. Returning an
// error here lets the caller decide whether to fail the request
// or proceed without idempotency.
func (s *JobStore) SaveIdempotency(ctx context.Context, userID, key, jobID string) error {
	if s.redis == nil || key == "" || userID == "" {
		return nil
	}
	return s.redis.Set(ctx, idempotencyKey(userID, key), jobID, idempotencyKeyTTL).Err()
}

func idempotencyKey(userID, key string) string {
	return idempotencyPrefix + userID + ":" + key
}

// Get returns the job. Redis-first when wired; Mongo on miss with
// repopulation back into Redis. Both miss → Mongo's typed
// not-found.
func (s *JobStore) Get(ctx context.Context, id string) (*Job, error) {
	// Fast path: Redis.
	if s.redis != nil {
		data, err := s.redis.Get(ctx, s.prefix+id).Bytes()
		if err == nil {
			var job Job
			if uerr := json.Unmarshal(data, &job); uerr == nil {
				return &job, nil
			}
			s.logger.Printf("outfit: redis job %s decoded as garbage; falling through", id)
		} else if !errors.Is(err, redis.Nil) {
			// Non-miss redis failure (network etc.) — log + fall through.
			s.logger.Printf("outfit: redis get job %s: %v (falling back to mongo)", id, err)
		}
	}

	// Source of truth.
	var job Job
	if err := s.mongoCol.FindOne(ctx, bson.M{"_id": id}).Decode(&job); err != nil {
		return nil, err
	}

	// Repopulate Redis so the next poll is fast again.
	if s.redis != nil {
		if data, err := json.Marshal(&job); err == nil {
			_ = s.redis.Set(ctx, s.prefix+id, data, s.redisTTL).Err()
		}
	}
	return &job, nil
}

// recoverStaleJobs marks any job in `processing` for too long as
// failed. Runs once at startup — captures jobs whose owning
// goroutine died with the previous backend process. Without this,
// an admin restarting the backend mid-generation leaves the user
// polling forever.
func recoverStaleJobs(ctx context.Context, col *mongo.Collection, logger *log.Logger) error {
	cutoff := time.Now().UTC().Add(-jobStaleProcessing)
	res, err := col.UpdateMany(ctx,
		bson.M{
			"status":    JobProcessing,
			"createdAt": bson.M{"$lt": cutoff},
		},
		bson.M{"$set": bson.M{
			"status":    JobFailed,
			"error":     "server restart — generation interrupted; please retry",
			"updatedAt": time.Now().UTC(),
		}},
	)
	if err != nil {
		return err
	}
	if res.ModifiedCount > 0 {
		logger.Printf("outfit: stale-job recovery: marked %d processing jobs as failed (server restart)", res.ModifiedCount)
	}
	return nil
}
