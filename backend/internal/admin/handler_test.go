package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// memoryRepo is a minimal in-memory Repository for tests. Intentionally
// simple — concurrency safety via a single mutex is enough for the
// serial test cases below; we're not stress-testing the persistence
// layer.
type memoryRepo struct {
	mu     sync.Mutex
	admins map[string]Admin        // keyed by email
	tokens map[string]RefreshToken // keyed by hash
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		admins: map[string]Admin{},
		tokens: map[string]RefreshToken{},
	}
}

func (m *memoryRepo) FindByEmail(ctx context.Context, email string) (*Admin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.admins[email]
	if !ok {
		return nil, nil
	}
	return &a, nil
}

func (m *memoryRepo) FindByID(ctx context.Context, id string) (*Admin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.admins {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, nil
}

func (m *memoryRepo) Create(ctx context.Context, a Admin) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.admins[a.Email] = a
	return nil
}

func (m *memoryRepo) UpdateLastActive(ctx context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for e, a := range m.admins {
		if a.ID == id {
			a.LastActiveAt = at
			m.admins[e] = a
			return nil
		}
	}
	return nil
}

func (m *memoryRepo) CountAdmins(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.admins)), nil
}

func (m *memoryRepo) SaveRefreshToken(ctx context.Context, t RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[t.ID] = t
	return nil
}

func (m *memoryRepo) FindRefreshToken(ctx context.Context, hash string) (*RefreshToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[hash]
	if !ok {
		return nil, nil
	}
	if t.RevokedAt != nil {
		return nil, nil
	}
	if time.Now().UTC().After(t.ExpiresAt) {
		return nil, nil
	}
	return &t, nil
}

func (m *memoryRepo) RevokeRefreshToken(ctx context.Context, hash string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tokens[hash]; ok {
		t.RevokedAt = &at
		m.tokens[hash] = t
	}
	return nil
}

func (m *memoryRepo) AppendAudit(ctx context.Context, e AuditEntry) error {
	// In-memory tests don't read audit; just no-op so the auth handler
	// surface stays compilable when audit calls are added later.
	return nil
}

func (m *memoryRepo) ListAudit(ctx context.Context, q AuditQuery) ([]AuditEntry, string, error) {
	// Empty stub — tests that exercise the audit-list handler should
	// either inject a real Mongo backed repo or mock at the handler
	// boundary.
	return nil, "", nil
}

// ── helpers ─────────────────────────────────────────────────────────────

func newTestHandler(t *testing.T) (*Handler, *memoryRepo) {
	t.Helper()
	repo := newMemoryRepo()
	// usersRepo + overviewRepo + tracesRepo are nil — these tests
	// only cover auth; the protected-endpoint tests mock those repos
	// separately.
	h := NewHandler(log.New(io.Discard, "", 0), repo, nil, nil, nil, testSecret)
	hash, err := HashPassword("hunter2hunter2")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_ = repo.Create(context.Background(), Admin{
		ID:           "adm_1",
		Email:        "admin@example.com",
		PasswordHash: hash,
		Roles:        []Role{RoleAdmin},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	})
	return h, repo
}

func doJSON(handler http.HandlerFunc, method, path, body string) (*httptest.ResponseRecorder, LoginResponse) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	var resp LoginResponse
	_ = json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&resp)
	return rec, resp
}

// ── tests ───────────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	rec, resp := doJSON(h.Login, http.MethodPost, "/admin/v1/auth/login",
		`{"email":"admin@example.com","password":"hunter2hunter2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", rec.Code, rec.Body)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatalf("expected both tokens in response; got %+v", resp)
	}
	if resp.Admin.Email != "admin@example.com" {
		t.Errorf("admin.email: got %q", resp.Admin.Email)
	}
	if len(resp.Admin.Roles) != 1 || resp.Admin.Roles[0] != "admin" {
		t.Errorf("admin.roles: got %v", resp.Admin.Roles)
	}
	// The access token must be parseable by the admin JWT validator.
	if _, err := ValidateToken(resp.AccessToken, testSecret); err != nil {
		t.Errorf("access token fails validation: %v", err)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h, _ := newTestHandler(t)
	rec, _ := doJSON(h.Login, http.MethodPost, "/admin/v1/auth/login",
		`{"email":"admin@example.com","password":"wrongwrongwr"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	h, _ := newTestHandler(t)
	rec, _ := doJSON(h.Login, http.MethodPost, "/admin/v1/auth/login",
		`{"email":"ghost@example.com","password":"hunter2hunter2"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestLogin_DisabledAdmin(t *testing.T) {
	h, repo := newTestHandler(t)
	// Flip the disabled flag.
	repo.mu.Lock()
	a := repo.admins["admin@example.com"]
	disabled := time.Now().UTC()
	a.DisabledAt = &disabled
	repo.admins["admin@example.com"] = a
	repo.mu.Unlock()

	rec, _ := doJSON(h.Login, http.MethodPost, "/admin/v1/auth/login",
		`{"email":"admin@example.com","password":"hunter2hunter2"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401 (disabled account must be indistinguishable from bad-creds)", rec.Code)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	h, _ := newTestHandler(t)
	rec, _ := doJSON(h.Login, http.MethodPost, "/admin/v1/auth/login",
		`{"email":"admin@example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestRefresh_RoundTrip_RotatesToken(t *testing.T) {
	h, _ := newTestHandler(t)
	// Login first to seed a refresh token.
	_, login := doJSON(h.Login, http.MethodPost, "/admin/v1/auth/login",
		`{"email":"admin@example.com","password":"hunter2hunter2"}`)
	firstRefresh := login.RefreshToken
	if firstRefresh == "" {
		t.Fatalf("login didn't return a refresh token")
	}

	// Refresh.
	body := `{"refreshToken":"` + firstRefresh + `"}`
	rec, refreshed := doJSON(h.Refresh, http.MethodPost, "/admin/v1/auth/refresh", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", rec.Code, rec.Body)
	}
	if refreshed.RefreshToken == "" || refreshed.AccessToken == "" {
		t.Fatalf("missing tokens in refresh response")
	}
	if refreshed.RefreshToken == firstRefresh {
		t.Errorf("refresh didn't rotate the token")
	}

	// Re-using the first refresh token must now fail — single-use.
	rec2, _ := doJSON(h.Refresh, http.MethodPost, "/admin/v1/auth/refresh", body)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("reusing an old refresh token returned %d, want 401", rec2.Code)
	}
}

func TestRefresh_UnknownToken(t *testing.T) {
	h, _ := newTestHandler(t)
	rec, _ := doJSON(h.Refresh, http.MethodPost, "/admin/v1/auth/refresh",
		`{"refreshToken":"deadbeef"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestRefresh_MissingToken(t *testing.T) {
	h, _ := newTestHandler(t)
	rec, _ := doJSON(h.Refresh, http.MethodPost, "/admin/v1/auth/refresh", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}
