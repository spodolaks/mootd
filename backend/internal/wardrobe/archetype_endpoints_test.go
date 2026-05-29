package wardrobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/pagination"
)

// fakeFillerSeeder is a stub ArchetypeFillerSeeder used by the
// endpoint tests. Captures the (userID, defaultID) call list so
// the assertions can verify what reached the seeder.
type fakeFillerSeeder struct {
	mu    sync.Mutex
	calls []struct{ userID, defaultID string }
	// returnID is the wi_<hex> the stub claims to have seeded.
	// Empty string with non-nil err simulates a backend failure.
	returnID string
	err      error
}

func (f *fakeFillerSeeder) SeedForUser(_ context.Context, userID, defaultID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct{ userID, defaultID string }{userID, defaultID})
	return f.returnID, f.err
}

// fakeRejectionsRepo is a stub ArchetypeRejectionsRepository.
// All operations are recorded so tests can assert on idempotency
// (Add called twice for the same key) and the post-claim cleanup
// (Delete fired after a successful seed, mootd#75).
type fakeRejectionsRepo struct {
	mu      sync.Mutex
	added   []string
	deleted []string
	addErr  error
}

func (f *fakeRejectionsRepo) Add(_ context.Context, userID, defaultID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, userID+":"+defaultID)
	return f.addErr
}

func (f *fakeRejectionsRepo) ListIDs(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (f *fakeRejectionsRepo) Delete(_ context.Context, userID, defaultID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, userID+":"+defaultID)
	return nil
}

// fakeWardrobeRepo lets us hand the handler a single canned item
// to "find" after the seeder reports success, without touching
// Mongo. Only the methods exercised by the endpoint paths are
// implemented; the rest panic so a future call from new code is
// loud.
type fakeWardrobeRepo struct {
	items []ClothingItem
}

func (f *fakeWardrobeRepo) Save(context.Context, ClothingItem) error { panic("Save unused in test") }
func (f *fakeWardrobeRepo) FindByUser(_ context.Context, _ string) ([]ClothingItem, error) {
	return f.items, nil
}
func (f *fakeWardrobeRepo) FindByUserPaginated(context.Context, string, int, *pagination.Cursor) ([]ClothingItem, error) {
	panic("unused")
}
func (f *fakeWardrobeRepo) FindBySeededDefault(context.Context, string, string) (*ClothingItem, error) {
	return nil, nil
}
func (f *fakeWardrobeRepo) OwnsItem(context.Context, string, string) (bool, error) {
	panic("unused")
}
func (f *fakeWardrobeRepo) UpdateItem(context.Context, string, string, map[string]string, string, string) error {
	panic("unused")
}
func (f *fakeWardrobeRepo) Delete(context.Context, string, string) error { panic("unused") }
func (f *fakeWardrobeRepo) DeleteAllByUser(context.Context, string) (int, error) {
	panic("unused")
}
func (f *fakeWardrobeRepo) SaveImage(context.Context, string, []byte, string) error { panic("unused") }
func (f *fakeWardrobeRepo) GetImage(context.Context, string) ([]byte, string, error) {
	panic("unused")
}
func (f *fakeWardrobeRepo) FindMissingPNG(_ context.Context, _ int, _ time.Duration) ([]ClothingItem, error) {
	panic("unused")
}
func (f *fakeWardrobeRepo) UpdatePngURL(context.Context, string, string) error { panic("unused") }
func (f *fakeWardrobeRepo) RecordPngFailure(context.Context, string, string) error {
	panic("unused")
}

// withUser stamps a userID into the request's context the way the
// auth middleware would in production.
func withUser(req *http.Request, userID string) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	return req.WithContext(ctx)
}

// newHandlerFor builds a minimal Handler wired with just the deps
// the archetype endpoints need. The other Handler fields stay nil
// because none of those code paths run here.
func newHandlerFor(seeder ArchetypeFillerSeeder, rejections ArchetypeRejectionsRepository, repo Repository) *Handler {
	logger := log.New(io.Discard, "", 0)
	h := &Handler{logger: logger, repo: repo}
	h.WithArchetypeEndpoints(ArchetypeEndpointsConfig{
		Seeder:     seeder,
		Rejections: rejections,
	})
	return h
}

// Test ArchetypeRejection: the happy path emits a 200 + records the
// rejection through the repo.
func TestArchetypeRejection_HappyPath(t *testing.T) {
	rejections := &fakeRejectionsRepo{}
	h := newHandlerFor(nil, rejections, nil)

	body := strings.NewReader(`{"defaultId":"ad_abc123"}`)
	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/wardrobe/archetype-rejections", body), "user_x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ArchetypeRejection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(rejections.added) != 1 || rejections.added[0] != "user_x:ad_abc123" {
		t.Errorf("expected one rejection added for user_x:ad_abc123, got %v", rejections.added)
	}
}

// Test ArchetypeRejection rejects bodies that don't carry the ad_
// prefix — defends against typos and the FE accidentally posting a
// wi_ id (which would silently corrupt the rejection list).
func TestArchetypeRejection_BadDefaultIDPrefix(t *testing.T) {
	rejections := &fakeRejectionsRepo{}
	h := newHandlerFor(nil, rejections, nil)

	for _, badID := range []string{"wi_oops", "", "abc", "AD_uppercase"} {
		body := strings.NewReader(`{"defaultId":"` + badID + `"}`)
		req := withUser(httptest.NewRequest(http.MethodPost, "/v1/wardrobe/archetype-rejections", body), "user_x")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.ArchetypeRejection(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("badID=%q expected 400 got %d body=%s", badID, rec.Code, rec.Body.String())
		}
	}
	if len(rejections.added) != 0 {
		t.Errorf("expected zero rejections to reach the repo from bad payloads; got %v", rejections.added)
	}
}

// Test 503 when the repo isn't wired — defensive degraded mode.
func TestArchetypeRejection_503WhenUnwired(t *testing.T) {
	h := newHandlerFor(nil, nil, nil) // both seeder + rejections nil

	body := strings.NewReader(`{"defaultId":"ad_abc123"}`)
	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/wardrobe/archetype-rejections", body), "user_x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ArchetypeRejection(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when rejections repo unwired, got %d", rec.Code)
	}
}

// Test 401 when the auth middleware didn't stamp a userID.
func TestArchetypeRejection_UnauthorizedWithoutUserID(t *testing.T) {
	rejections := &fakeRejectionsRepo{}
	h := newHandlerFor(nil, rejections, nil)

	body := strings.NewReader(`{"defaultId":"ad_abc"}`)
	// NB: no withUser wrapper.
	req := httptest.NewRequest(http.MethodPost, "/v1/wardrobe/archetype-rejections", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ArchetypeRejection(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without userID, got %d", rec.Code)
	}
}

// Test FromArchetypeDefault returns 503 without a seeder wired.
func TestFromArchetypeDefault_503WhenSeederUnwired(t *testing.T) {
	h := newHandlerFor(nil, nil, nil)

	body := strings.NewReader(`{"defaultId":"ad_abc"}`)
	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/wardrobe/items/from-archetype-default", body), "user_x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.FromArchetypeDefault(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 with seeder unwired, got %d", rec.Code)
	}
}

// Test FromArchetypeDefault validates the ad_ prefix.
func TestFromArchetypeDefault_BadDefaultID(t *testing.T) {
	seeder := &fakeFillerSeeder{returnID: "wi_xyz"}
	rejections := &fakeRejectionsRepo{}
	h := newHandlerFor(seeder, rejections, &fakeWardrobeRepo{})

	body := strings.NewReader(`{"defaultId":"wi_not_a_default"}`)
	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/wardrobe/items/from-archetype-default", body), "user_x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.FromArchetypeDefault(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad prefix, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(seeder.calls) != 0 {
		t.Errorf("seeder should NOT have been called for bad prefix; got %v", seeder.calls)
	}
}

// Test the seeder error path — backend failure surfaces as 500.
func TestFromArchetypeDefault_SeederErrorIs500(t *testing.T) {
	seeder := &fakeFillerSeeder{err: errors.New("mongo down")}
	rejections := &fakeRejectionsRepo{}
	h := newHandlerFor(seeder, rejections, &fakeWardrobeRepo{})

	body := strings.NewReader(`{"defaultId":"ad_abc"}`)
	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/wardrobe/items/from-archetype-default", body), "user_x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.FromArchetypeDefault(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when seeder errors, got %d body=%s", rec.Code, rec.Body.String())
	}
	// Seeder was attempted exactly once.
	if len(seeder.calls) != 1 {
		t.Errorf("expected 1 seeder call, got %d (%v)", len(seeder.calls), seeder.calls)
	}
	// Rejection cleanup should NOT fire when the seed failed.
	if len(rejections.deleted) != 0 {
		t.Errorf("rejection cleanup should not run on seed failure; got %v", rejections.deleted)
	}
}

// Test the happy path: seeder success → returned ClothingItem +
// stale rejection cleared (mootd#75).
func TestFromArchetypeDefault_HappyPathClearsStaleRejection(t *testing.T) {
	const seededID = "wi_seed_42"
	seeder := &fakeFillerSeeder{returnID: seededID}
	rejections := &fakeRejectionsRepo{}
	repo := &fakeWardrobeRepo{
		items: []ClothingItem{{
			ID:       seededID,
			UserID:   "user_x",
			Category: "outerwear",
			Label:    "Black bomber",
			Traits:   map[string]string{"seededFromDefaultId": "ad_abc"},
		}},
	}
	h := newHandlerFor(seeder, rejections, repo)

	body := strings.NewReader(`{"defaultId":"ad_abc"}`)
	req := withUser(httptest.NewRequest(http.MethodPost, "/v1/wardrobe/items/from-archetype-default", body), "user_x")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.FromArchetypeDefault(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp FromArchetypeDefaultResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Item.ID != seededID {
		t.Errorf("expected returned item id %q, got %q", seededID, resp.Item.ID)
	}
	// mootd#75 — stale rejection for the same default should be
	// cleared post-claim so the data model stays consistent.
	if len(rejections.deleted) != 1 || rejections.deleted[0] != "user_x:ad_abc" {
		t.Errorf("expected stale rejection cleanup for user_x:ad_abc; got %v", rejections.deleted)
	}
}
