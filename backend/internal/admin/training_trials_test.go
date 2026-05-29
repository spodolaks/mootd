package admin

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// memTrainingTrials is an in-memory TrainingTrialsRepository for the
// handler tests. Mirrors memoryRepo's single-mutex simplicity.
type memTrainingTrials struct {
	mu   sync.Mutex
	rows map[string]TrainingTrial
}

func newMemTrainingTrials() *memTrainingTrials {
	return &memTrainingTrials{rows: map[string]TrainingTrial{}}
}

func (m *memTrainingTrials) CreateTrainingTrial(ctx context.Context, t TrainingTrial) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[t.ID]; ok {
		return context.DeadlineExceeded // any non-nil err triggers the handler's idempotent Get path
	}
	m.rows[t.ID] = t
	return nil
}

func (m *memTrainingTrials) GetTrainingTrial(ctx context.Context, id string) (*TrainingTrial, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.rows[id]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func (m *memTrainingTrials) ListTrainingTrials(ctx context.Context, q TrainingTrialQuery) ([]TrainingTrial, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var out []TrainingTrial
	for _, t := range m.rows {
		if q.Status != "" && t.Status != q.Status {
			continue
		}
		if q.Cursor != "" && t.ID >= q.Cursor {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	next := ""
	if len(out) > limit {
		next = out[limit-1].ID
		out = out[:limit]
	}
	return out, next, nil
}

func (m *memTrainingTrials) SubmitTrainingTrial(ctx context.Context, id, submittedBy string, in TrainingSubmitInput, at time.Time) (*TrainingTrial, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	picks := in.Picks
	if picks == nil {
		picks = map[string]string{}
	}
	t, ok := m.rows[id]
	if !ok {
		t = TrainingTrial{ID: id, CreatedBy: submittedBy, CreatedAt: at}
	}
	t.Status = TrainingStatusSubmitted
	t.SubmittedBy = submittedBy
	t.SubmittedAt = &at
	t.Picks = picks
	t.CustomValues = in.CustomValues
	t.PickCount = len(picks)
	t.AttrCount = in.AttrCount
	if in.ClaudeDescription != nil {
		t.ClaudeDescription = in.ClaudeDescription
	}
	if in.GemmaDescription != nil {
		t.GemmaDescription = in.GemmaDescription
	}
	if in.SourceImageURL != "" {
		t.SourceImageURL = in.SourceImageURL
	}
	if in.ClaudeRequestID != "" {
		t.ClaudeRequestID = in.ClaudeRequestID
	}
	if in.GemmaRequestID != "" {
		t.GemmaRequestID = in.GemmaRequestID
	}
	m.rows[id] = t
	return &t, nil
}

// trainingTestHandler builds a Handler wired with the in-memory admin
// repo (for audit) + an in-memory training-trials store.
func trainingTestHandler(tt TrainingTrialsRepository) *Handler {
	repo := newMemoryRepo()
	h := NewHandler(log.New(io.Discard, "", 0), repo, nil, nil, nil, "test-secret-not-equal-to-jwt")
	if tt != nil {
		h.WithTrainingTrials(tt)
	}
	return h
}

func trainingReq(method, target, body string, roles ...Role) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	rs := make([]string, len(roles))
	for i, role := range roles {
		rs[i] = string(role)
	}
	return r.WithContext(ContextWithAuth(r.Context(), "admin-1", rs, true))
}

func TestTrainingSubmissions_503WhenUnwired(t *testing.T) {
	h := trainingTestHandler(nil) // no training store
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/submissions", "", RoleEngineer))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when store unwired, got %d", rr.Code)
	}
}

func TestTrainingSubmissions_CreateListGet(t *testing.T) {
	h := trainingTestHandler(newMemTrainingTrials())

	// Create
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodPost, "/admin/v1/training/submissions",
		`{"trialId":"trial-abc","label":"shirt.jpg"}`, RoleEngineer))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var created TrainingTrial
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("create decode: %v", err)
	}
	if created.Status != TrainingStatusInReview || created.CreatedBy != "admin-1" {
		t.Fatalf("create: unexpected record %+v", created)
	}

	// Create with missing trialId → 400
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodPost, "/admin/v1/training/submissions", `{"label":"x"}`, RoleEngineer))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create empty id: want 400, got %d", rr.Code)
	}

	// Create without rerun permission → 403
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodPost, "/admin/v1/training/submissions", `{"trialId":"trial-x"}`))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("create no-perm: want 403, got %d", rr.Code)
	}

	// List → contains trial-abc
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/submissions", "", RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", rr.Code)
	}
	var page TrainingTrialPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	if len(page.Trials) != 1 || page.Trials[0].ID != "trial-abc" {
		t.Fatalf("list: unexpected page %+v", page)
	}

	// Get one
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/submissions/trial-abc", "", RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d", rr.Code)
	}

	// Get unknown → 404
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/submissions/trial-nope", "", RoleEngineer))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get unknown: want 404, got %d", rr.Code)
	}
}

func TestTrainingSubmissions_Submit(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)

	// Submit without rerun permission → 403
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodPost, "/admin/v1/training/submissions/trial-s/submit",
		`{"picks":{"color":"claude"},"attrCount":3}`))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("submit no-perm: want 403, got %d", rr.Code)
	}

	// Submit (upsert) → 200, status submitted, picks + custom value recorded
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodPost, "/admin/v1/training/submissions/trial-s/submit",
		`{"picks":{"color":"claude","fit":"gemma","material":"custom"},"customValues":{"material":"waxed canvas"},"attrCount":5}`, RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("submit: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var rec TrainingTrial
	if err := json.Unmarshal(rr.Body.Bytes(), &rec); err != nil {
		t.Fatalf("submit decode: %v", err)
	}
	if rec.Status != TrainingStatusSubmitted {
		t.Fatalf("submit: want status submitted, got %q", rec.Status)
	}
	if rec.PickCount != 3 || rec.AttrCount != 5 || rec.SubmittedBy != "admin-1" {
		t.Fatalf("submit: unexpected record %+v", rec)
	}
	if rec.CustomValues["material"] != "waxed canvas" {
		t.Fatalf("submit: custom value not recorded, got %+v", rec.CustomValues)
	}
}

// TestTrainingSubmissions_Submit_Snapshot verifies the Phase 1 snapshot
// (mootd-admin#124): both describers' full structured descriptions plus
// provenance are persisted on submit, so the record reconstructs a
// (chosen, rejected) pair without re-reading the orchestrator.
func TestTrainingSubmissions_Submit_Snapshot(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)

	body := `{
		"picks":{"color_primary":"claude"},
		"attrCount":1,
		"claudeDescription":{"color_primary":"navy","fit":"slim"},
		"gemmaDescription":{"color_primary":"blue","fit":"slim"},
		"sourceImageUrl":"gridfs://sid_uploads/0123456789abcdef01234567",
		"claudeRequestId":"trial-z-claude",
		"gemmaRequestId":"trial-z-gemma"
	}`
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodPost, "/admin/v1/training/submissions/trial-z/submit", body, RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("submit: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	rec, err := store.GetTrainingTrial(context.Background(), "trial-z")
	if err != nil || rec == nil {
		t.Fatalf("get after submit: %v rec=%v", err, rec)
	}
	if rec.ClaudeDescription["color_primary"] != "navy" {
		t.Fatalf("claude snapshot not persisted: %+v", rec.ClaudeDescription)
	}
	if rec.GemmaDescription["color_primary"] != "blue" {
		t.Fatalf("gemma snapshot not persisted: %+v", rec.GemmaDescription)
	}
	if rec.SourceImageURL == "" || rec.ClaudeRequestID == "" || rec.GemmaRequestID == "" {
		t.Fatalf("provenance not persisted: %+v", rec)
	}
	// The picked side is claude → chosen "navy", rejected "blue":
	// reconstructable from the record alone, the Phase 1 exit criterion.
	if rec.Picks["color_primary"] != "claude" {
		t.Fatalf("pick not recorded: %+v", rec.Picks)
	}
}
