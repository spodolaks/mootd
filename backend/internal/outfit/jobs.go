package outfit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// JobStatus represents the state of an async outfit generation job.
type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobProcessing JobStatus = "processing"
	JobCompleted  JobStatus = "completed"
	JobFailed     JobStatus = "failed"
)

// Job represents an async outfit generation job stored in Redis.
type Job struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userID"`
	Status    JobStatus `json:"status"`
	Outfits   []Outfit  `json:"outfits,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// JobStore manages async outfit generation jobs in Redis.
type JobStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

func NewJobStore(client *redis.Client) *JobStore {
	return &JobStore{
		client: client,
		prefix: "outfit:job:",
		ttl:    10 * time.Minute,
	}
}

func (s *JobStore) Save(ctx context.Context, job *Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.prefix+job.ID, data, s.ttl).Err()
}

func (s *JobStore) Get(ctx context.Context, id string) (*Job, error) {
	data, err := s.client.Get(ctx, s.prefix+id).Bytes()
	if err != nil {
		return nil, err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}
