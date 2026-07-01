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

// stubUsersRepo implements UsersRepository; only ListSummaries and
// FindDetail return data. The rest are no-ops — enough to exercise the
// handlers' redaction / PII-gate branches.
type stubUsersRepo struct {
	summaries []UserSummary
	detail    *UserDetail
}

func (s *stubUsersRepo) ListSummaries(context.Context, UsersQuery) ([]UserSummary, string, error) {
	// Return a fresh copy each call, like the real Mongo repo (which decodes
	// new structs per query). ListUsers redacts in place, so a shared backing
	// array would let one call's redaction leak into the next.
	out := make([]UserSummary, len(s.summaries))
	copy(out, s.summaries)
	return out, "", nil
}
func (s *stubUsersRepo) FindDetail(context.Context, string) (*UserDetail, error) {
	if s.detail == nil {
		return nil, nil
	}
	// Fresh copy per call, same reasoning as ListSummaries: the
	// handler redacts in place.
	d := *s.detail
	return &d, nil
}
func (s *stubUsersRepo) SearchUsers(context.Context, string, int) ([]SearchHit, error) {
	return nil, nil
}
func (s *stubUsersRepo) ListWardrobe(context.Context, string, string, int) ([]UserWardrobeItem, string, error) {
	return nil, "", nil
}
func (s *stubUsersRepo) ListMoodboards(context.Context, string, string, int) ([]UserMoodboard, string, error) {
	return nil, "", nil
}
func (s *stubUsersRepo) SpendBreakdown(context.Context, string, time.Time) (*UserSpendBreakdown, error) {
	return nil, nil
}
func (s *stubUsersRepo) ListOutfitBatches(context.Context, string, string, int) ([]UserOutfitBatch, string, error) {
	return nil, "", nil
}

func listUsersAs(t *testing.T, h *Handler, role Role) UsersListResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/users", nil)
	req = req.WithContext(ContextWithAuth(req.Context(), "admin1", []string{string(role)}, false))
	rec := httptest.NewRecorder()
	h.ListUsers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListUsers status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp UsersListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestListUsers_RedactsEmailWithoutUsersPII(t *testing.T) {
	h := &Handler{
		logger:    log.New(io.Discard, "", 0),
		usersRepo: &stubUsersRepo{summaries: []UserSummary{{ID: "u1", Email: "jane@example.com"}}},
	}

	// engineer has users:read (so it reaches the handler) but NOT users:pii.
	got := listUsersAs(t, h, RoleEngineer)
	if got.Users[0].Email != redactedEmail {
		t.Errorf("engineer saw email %q, want it redacted to %q", got.Users[0].Email, redactedEmail)
	}

	// support holds users:pii → email is shown in the clear.
	got = listUsersAs(t, h, RoleSupport)
	if got.Users[0].Email != "jane@example.com" {
		t.Errorf("support saw email %q, want cleartext", got.Users[0].Email)
	}
}
