package wardrobe

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// DetectJobStatus represents the state of an async clothing-detection job.
// Mirrors outfit.JobStatus so the two async flows look identical from the
// client's perspective.
type DetectJobStatus string

const (
	DetectJobPending    DetectJobStatus = "pending"
	DetectJobProcessing DetectJobStatus = "processing"
	DetectJobCompleted  DetectJobStatus = "completed"
	DetectJobFailed     DetectJobStatus = "failed"
)

// DetectJob holds the lifecycle + result of a detection request. Image bytes
// are deliberately NOT stored here — the worker captures them from the POST
// body and keeps them in its own goroutine closure, so a ~5MB photo never
// round-trips through Redis.
type DetectJob struct {
	ID        string          `json:"id"`
	UserID    string          `json:"userID"`
	Status    DetectJobStatus `json:"status"`
	Items     []DetectedItem  `json:"items,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// DetectJobStore manages async detection jobs in Redis. Entries auto-expire
// after the TTL so abandoned jobs (client crashed mid-poll) don't linger.
type DetectJobStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewDetectJobStore constructs a DetectJobStore. The 10-minute TTL covers
// the worst-case detection run plus a generous poll grace period; outfit's
// equivalent store uses the same value to keep behaviour symmetrical.
func NewDetectJobStore(client *redis.Client) *DetectJobStore {
	return &DetectJobStore{
		client: client,
		prefix: "wardrobe:detect:job:",
		ttl:    10 * time.Minute,
	}
}

// Save upserts the job under its ID with the store's TTL.
func (s *DetectJobStore) Save(ctx context.Context, job *DetectJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.prefix+job.ID, data, s.ttl).Err()
}

// Get loads the job by ID. Returns redis.Nil wrapped error when missing.
func (s *DetectJobStore) Get(ctx context.Context, id string) (*DetectJob, error) {
	data, err := s.client.Get(ctx, s.prefix+id).Bytes()
	if err != nil {
		return nil, err
	}
	var job DetectJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}
