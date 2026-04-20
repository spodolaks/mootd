package moodboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/pagination"
	"mootd/backend/internal/wardrobe"
)

// ── Test doubles ────────────────────────────────────────────────────────────

var errBoom = errors.New("boom")

// stubRepo is the minimum Repository implementation needed for Save tests.
// Only Save + DeleteAllByUser have real bodies; the rest satisfy the
// interface so we can hand this to NewHandler.
type stubRepo struct {
	saved []SavedMoodBoard
	err   error
}

func (s *stubRepo) Save(_ context.Context, b SavedMoodBoard) error {
	if s.err != nil {
		return s.err
	}
	s.saved = append(s.saved, b)
	return nil
}
func (s *stubRepo) FindByUser(_ context.Context, _ string) ([]SavedMoodBoard, error) {
	return nil, nil
}
func (s *stubRepo) FindByUserPaginated(_ context.Context, _ string, _ int, _ *pagination.Cursor) ([]SavedMoodBoard, error) {
	return nil, nil
}
func (s *stubRepo) FindRecent(_ context.Context, _ string, _ int) ([]SavedMoodBoard, error) {
	return nil, nil
}
func (s *stubRepo) DeleteAllByUser(_ context.Context, _ string) (int, error) { return 0, nil }

type fakeWardrobeRepo struct{}

func (fakeWardrobeRepo) FindByUser(_ context.Context, _ string) ([]wardrobe.ClothingItem, error) {
	return nil, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func authedPost(t *testing.T, userID string, body any) *http.Request {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/moodboards", bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	return r.WithContext(ctx)
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestSave_InvokesOnSaveWithFullRequest(t *testing.T) {
	repo := &stubRepo{}
	var gotReq SaveRequest
	var gotSaved SavedMoodBoard
	var gotUser string
	hook := SaveEventFn(func(_ context.Context, userID string, req SaveRequest, saved SavedMoodBoard) {
		gotUser = userID
		gotReq = req
		gotSaved = saved
	})

	h := NewHandler(log.New(io.Discard, "", 0), repo, fakeWardrobeRepo{}, nil, hook)

	body := map[string]any{
		"outfit": map[string]any{
			"id":          "outfit_chosen",
			"name":        "Saturday Casual",
			"items":       []string{"i1", "i2"},
			"description": "soft tee + cropped jeans",
		},
		"date":  "2026-04-20",
		"jobId": "job_abc",
		"generatedBatch": []map[string]any{
			{"id": "outfit_chosen", "name": "Saturday Casual", "items": []string{"i1", "i2"}, "description": "d"},
			{"id": "outfit_b", "name": "Brunch", "items": []string{"i3", "i4"}, "description": "d"},
			{"id": "outfit_c", "name": "Errands", "items": []string{"i5", "i6"}, "description": "d"},
		},
	}

	rec := httptest.NewRecorder()
	h.Save(rec, authedPost(t, "user_xyz", body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if gotUser != "user_xyz" {
		t.Errorf("hook userID = %q, want user_xyz", gotUser)
	}
	if gotReq.Outfit.ID != "outfit_chosen" {
		t.Errorf("chosen outfit ID = %q, want outfit_chosen", gotReq.Outfit.ID)
	}
	if gotReq.JobID != "job_abc" {
		t.Errorf("jobID = %q, want job_abc", gotReq.JobID)
	}
	if len(gotReq.GeneratedBatch) != 3 {
		t.Errorf("batch size = %d, want 3", len(gotReq.GeneratedBatch))
	}
	if gotSaved.UserID != "user_xyz" || gotSaved.ID == "" {
		t.Errorf("saved board not populated: %+v", gotSaved)
	}
}

func TestSave_NilHookIsSkippedCleanly(t *testing.T) {
	repo := &stubRepo{}
	h := NewHandler(log.New(io.Discard, "", 0), repo, fakeWardrobeRepo{}, nil, nil)

	body := map[string]any{
		"outfit": map[string]any{"name": "X", "items": []string{"a"}, "description": "d"},
		"date":   "2026-04-20",
	}

	rec := httptest.NewRecorder()
	h.Save(rec, authedPost(t, "u1", body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSave_HookFiresOnlyAfterRepoSucceeds(t *testing.T) {
	repo := &stubRepo{err: errBoom}
	called := false
	hook := SaveEventFn(func(_ context.Context, _ string, _ SaveRequest, _ SavedMoodBoard) {
		called = true
	})

	h := NewHandler(log.New(io.Discard, "", 0), repo, fakeWardrobeRepo{}, nil, hook)

	body := map[string]any{
		"outfit": map[string]any{"name": "X", "items": []string{"a"}, "description": "d"},
		"date":   "2026-04-20",
	}

	rec := httptest.NewRecorder()
	h.Save(rec, authedPost(t, "u1", body))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("hook was invoked despite save failure — feedback would record a phantom event")
	}
}
