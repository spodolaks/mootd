package admin

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// piiStubUsersRepo returns canned wardrobe / moodboard / outfit content so the
// sub-resource redaction branches in getUserWardrobe/getUserMoodboards/
// getUserOutfits can be exercised. Only the three list methods carry data; the
// rest are no-ops.
type piiStubUsersRepo struct {
	wardrobe   []UserWardrobeItem
	moodboards []UserMoodboard
	batches    []UserOutfitBatch
}

func (s *piiStubUsersRepo) ListSummaries(context.Context, UsersQuery) ([]UserSummary, string, error) {
	return nil, "", nil
}
func (s *piiStubUsersRepo) FindDetail(context.Context, string) (*UserDetail, error) { return nil, nil }
func (s *piiStubUsersRepo) SearchUsers(context.Context, string, int) ([]SearchHit, error) {
	return nil, nil
}
func (s *piiStubUsersRepo) ListWardrobe(context.Context, string, string, int) ([]UserWardrobeItem, string, error) {
	out := make([]UserWardrobeItem, len(s.wardrobe))
	copy(out, s.wardrobe)
	return out, "", nil
}
func (s *piiStubUsersRepo) ListMoodboards(context.Context, string, string, int) ([]UserMoodboard, string, error) {
	out := make([]UserMoodboard, len(s.moodboards))
	copy(out, s.moodboards)
	return out, "", nil
}
func (s *piiStubUsersRepo) SpendBreakdown(context.Context, string, time.Time) (*UserSpendBreakdown, error) {
	return nil, nil
}
func (s *piiStubUsersRepo) ListOutfitBatches(context.Context, string, string, int) ([]UserOutfitBatch, string, error) {
	out := make([]UserOutfitBatch, len(s.batches))
	copy(out, s.batches)
	return out, "", nil
}

// stubDetectionRuns implements DetectionRunRepository. GetInputImage returns a
// sentinel payload so a successful stream is distinguishable from a 403.
type stubDetectionRuns struct{}

func (stubDetectionRuns) FindRun(context.Context, string) (*DetectionRun, error) { return nil, nil }
func (stubDetectionRuns) GetInputImage(context.Context, string) ([]byte, string, error) {
	return []byte("JPEGDATA"), "image/jpeg", nil
}
func (stubDetectionRuns) ListVersions(context.Context) ([]string, error) { return nil, nil }
func (stubDetectionRuns) Rerun(context.Context, string, string, string) (string, error) {
	return "", nil
}

func detectionInputImageAs(t *testing.T, h *Handler, role Role) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/detection-runs/run123/input-image", nil)
	req = req.WithContext(ContextWithAuth(req.Context(), "admin1", []string{string(role)}, false))
	rec := httptest.NewRecorder()
	h.GetDetectionRun(rec, req)
	return rec
}

func TestDetectionInputImage_RequiresUsersPII(t *testing.T) {
	h := &Handler{
		logger:        log.New(io.Discard, "", 0),
		detectionRuns: stubDetectionRuns{},
	}

	// engineer has traces:read (so the route would let it through) but NOT
	// users:pii — the raw user photo must be refused with 403.
	rec := detectionInputImageAs(t, h, RoleEngineer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("engineer status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 403 body: %v", err)
	}
	if body["missingPermission"] != string(PermUsersPII) {
		t.Errorf("missingPermission = %v, want %q", body["missingPermission"], PermUsersPII)
	}

	// readonly likewise lacks users:pii.
	if rec := detectionInputImageAs(t, h, RoleReadonly); rec.Code != http.StatusForbidden {
		t.Errorf("readonly status = %d, want 403", rec.Code)
	}

	// support holds users:pii → the photo streams (200 + bytes).
	rec = detectionInputImageAs(t, h, RoleSupport)
	if rec.Code != http.StatusOK {
		t.Fatalf("support status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "JPEGDATA" {
		t.Errorf("support body = %q, want the streamed image bytes", rec.Body.String())
	}
}

func getUserSubResourceAs(t *testing.T, h *Handler, path string, role Role) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(ContextWithAuth(req.Context(), "admin1", []string{string(role)}, false))
	rec := httptest.NewRecorder()
	h.GetUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want 200 (body=%s)", path, rec.Code, rec.Body.String())
	}
	return rec
}

func TestGetUserWardrobe_RedactsContentWithoutUsersPII(t *testing.T) {
	repo := &piiStubUsersRepo{wardrobe: []UserWardrobeItem{{
		ID:          "w1",
		Category:    "tops",
		Label:       "Vintage band tee",
		ImageURL:    "https://cdn/w1.jpg",
		PngImageURL: "https://cdn/w1.png",
		Traits:      map[string]string{"color": "black"},
	}}}
	h := &Handler{logger: log.New(io.Discard, "", 0), usersRepo: repo}

	// engineer: users:read but not users:pii — label + image URLs redacted,
	// non-content scaffolding preserved.
	rec := getUserSubResourceAs(t, h, "/admin/v1/users/u1/wardrobe", RoleEngineer)
	var page UserWardrobePage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := page.Items[0]
	if got.Label != redactedLabel {
		t.Errorf("label = %q, want redacted %q", got.Label, redactedLabel)
	}
	if got.ImageURL != "" || got.PngImageURL != "" {
		t.Errorf("image URLs not blanked: imageUrl=%q pngImageUrl=%q", got.ImageURL, got.PngImageURL)
	}
	if got.ID != "w1" || got.Category != "tops" || got.Traits["color"] != "black" {
		t.Errorf("non-content fields wrongly altered: %+v", got)
	}

	// support holds users:pii → content is in the clear.
	rec = getUserSubResourceAs(t, h, "/admin/v1/users/u1/wardrobe", RoleSupport)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Items[0].Label != "Vintage band tee" || page.Items[0].ImageURL != "https://cdn/w1.jpg" {
		t.Errorf("support saw redacted content: %+v", page.Items[0])
	}
}

func TestGetUserMoodboards_RedactsContentWithoutUsersPII(t *testing.T) {
	repo := &piiStubUsersRepo{moodboards: []UserMoodboard{{
		ID:       "m1",
		UserID:   "u1",
		Date:     "2026-06-01",
		ImageURL: "https://cdn/m1.png",
		Outfit:   map[string]any{"name": "Casual Friday", "snapshots": []any{map[string]any{"label": "tee", "imageUrl": "https://cdn/w1.jpg"}}},
	}}}
	h := &Handler{logger: log.New(io.Discard, "", 0), usersRepo: repo}

	rec := getUserSubResourceAs(t, h, "/admin/v1/users/u1/moodboards", RoleEngineer)
	var page UserMoodboardsPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := page.Items[0]
	if got.ImageURL != "" {
		t.Errorf("collage imageUrl not blanked: %q", got.ImageURL)
	}
	if got.Outfit != nil {
		t.Errorf("outfit payload not dropped: %+v", got.Outfit)
	}
	if got.ID != "m1" || got.Date != "2026-06-01" {
		t.Errorf("non-content fields wrongly altered: %+v", got)
	}

	rec = getUserSubResourceAs(t, h, "/admin/v1/users/u1/moodboards", RoleSupport)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Items[0].ImageURL != "https://cdn/m1.png" || page.Items[0].Outfit == nil {
		t.Errorf("support saw redacted moodboard content: %+v", page.Items[0])
	}
}

func TestGetUserOutfits_RedactsContentWithoutUsersPII(t *testing.T) {
	repo := &piiStubUsersRepo{batches: []UserOutfitBatch{{
		ID:         "b1",
		UserID:     "u1",
		Status:     "completed",
		CreatedAt:  time.Now().UTC(),
		Candidates: []map[string]any{{"name": "Look 1", "itemSnapshots": []any{map[string]any{"label": "tee", "imageUrl": "https://cdn/w1.jpg"}}}},
	}}}
	h := &Handler{logger: log.New(io.Discard, "", 0), usersRepo: repo}

	rec := getUserSubResourceAs(t, h, "/admin/v1/users/u1/outfits", RoleEngineer)
	var page UserOutfitsPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := page.Batches[0]
	if got.Candidates != nil {
		t.Errorf("candidates not dropped: %+v", got.Candidates)
	}
	if got.ID != "b1" || got.Status != "completed" {
		t.Errorf("non-content fields wrongly altered: %+v", got)
	}

	rec = getUserSubResourceAs(t, h, "/admin/v1/users/u1/outfits", RoleSupport)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Batches[0].Candidates == nil {
		t.Errorf("support saw redacted outfit candidates: %+v", page.Batches[0])
	}
}
