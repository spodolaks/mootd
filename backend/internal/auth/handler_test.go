package auth

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

	jwtutil "mootd/backend/internal/shared/jwt"
)

// ---------------------------------------------------------------------------
// In-memory fake Repository
// ---------------------------------------------------------------------------

// fakeAuthRepo is a hand-written in-memory implementation of the auth
// Repository interface. It models the single moving part the handlers care
// about — the refresh-token hash currently bound to each user — plus enough
// bookkeeping to assert rotation, revocation, and upsert behaviour.
//
// The real MongoRepository stores at most one refreshTokenHash per user
// document, so this fake mirrors that: SaveRefreshToken overwrites, and
// FindByRefreshToken / ClearRefreshTokenByHash reverse-scan the userID→hash
// map. Methods are guarded by a mutex so the fake is safe to share if a test
// ever exercises the handler concurrently.
type fakeAuthRepo struct {
	mu sync.Mutex

	// hashByUser maps userID -> currently-valid refresh-token hash.
	hashByUser map[string]string
	// expiryByUser maps userID -> refresh-token expiry (mirrors the doc field).
	expiryByUser map[string]time.Time
	// users maps userID -> profile, populated by UpsertByGoogleID and read by
	// FindByRefreshToken so the refresh response can echo the stored profile.
	users map[string]*UserDocument

	// upsertCalls records each UpsertByGoogleID invocation for assertions.
	upsertCalls []upsertCall

	// findErr, when set, is returned by FindByRefreshToken to exercise the
	// handler's 500 (repository failure) branch.
	findErr error
	// saveErr, when set, is returned by SaveRefreshToken.
	saveErr error
}

type upsertCall struct {
	googleID, email, name, avatarURL string
}

func newFakeAuthRepo() *fakeAuthRepo {
	return &fakeAuthRepo{
		hashByUser:   map[string]string{},
		expiryByUser: map[string]time.Time{},
		users:        map[string]*UserDocument{},
	}
}

func (f *fakeAuthRepo) UpsertByGoogleID(_ context.Context, googleID, email, name, avatarURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsertCalls = append(f.upsertCalls, upsertCall{googleID, email, name, avatarURL})
	doc, ok := f.users[googleID]
	if !ok {
		doc = &UserDocument{ID: googleID, GoogleID: googleID, CreatedAt: time.Now().UTC()}
		f.users[googleID] = doc
	}
	doc.Email = email
	doc.Name = name
	doc.AvatarURL = avatarURL
	doc.UpdatedAt = time.Now().UTC()
	return nil
}

func (f *fakeAuthRepo) SaveRefreshToken(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	// Ensure a user doc exists so a subsequent FindByRefreshToken can echo a
	// profile (mock-login users are never upserted via Google).
	if _, ok := f.users[userID]; !ok {
		f.users[userID] = &UserDocument{ID: userID}
	}
	f.hashByUser[userID] = tokenHash
	f.expiryByUser[userID] = expiresAt
	return nil
}

func (f *fakeAuthRepo) FindByRefreshToken(_ context.Context, tokenHash string) (*UserDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.findErr != nil {
		return nil, f.findErr
	}
	for userID, h := range f.hashByUser {
		if h != tokenHash {
			continue
		}
		// Mirror the Mongo query's expiry predicate: an expired hash matches
		// nothing.
		if exp, ok := f.expiryByUser[userID]; ok && !exp.After(time.Now().UTC()) {
			return nil, nil
		}
		if doc, ok := f.users[userID]; ok {
			cp := *doc
			return &cp, nil
		}
		return &UserDocument{ID: userID}, nil
	}
	return nil, nil
}

func (f *fakeAuthRepo) ClearRefreshToken(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.hashByUser, userID)
	delete(f.expiryByUser, userID)
	return nil
}

func (f *fakeAuthRepo) ClearRefreshTokenByHash(_ context.Context, tokenHash string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for userID, h := range f.hashByUser {
		if h == tokenHash {
			delete(f.hashByUser, userID)
			delete(f.expiryByUser, userID)
			return true, nil
		}
	}
	return false, nil
}

// hashFor returns the currently-stored refresh-token hash for userID (test-only).
func (f *fakeAuthRepo) hashFor(userID string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.hashByUser[userID]
	return h, ok
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	authTestSecret  = "handler-test-secret-min-32-characters!"
	mockLoginUserID = "user_mock_001"
)

func newTestHandler(repo Repository) *Handler {
	return NewHandler(log.New(io.Discard, "", 0), repo, authTestSecret, []string{mootdClientID})
}

// doJSON builds a request with the given method/body and runs it through fn,
// returning the recorder. A nil body sends no Content-Length body.
func doJSON(fn http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fn(rec, r)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return out
}

// ---------------------------------------------------------------------------
// MockLogin
// ---------------------------------------------------------------------------

func TestMockLogin_Success(t *testing.T) {
	repo := newFakeAuthRepo()
	h := newTestHandler(repo)

	rec := doJSON(h.MockLogin, http.MethodPost, "/v1/auth/mock-login", `{"provider":"google"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	resp := decodeBody[MockLoginResponse](t, rec)
	if resp.AccessToken == "" {
		t.Error("accessToken is empty")
	}
	if resp.RefreshToken == "" {
		t.Error("refreshToken is empty")
	}
	if resp.Mode != "mock" {
		t.Errorf("mode = %q, want %q", resp.Mode, "mock")
	}
	if resp.User.ID != mockLoginUserID {
		t.Errorf("user.id = %q, want %q", resp.User.ID, mockLoginUserID)
	}
	if resp.User.Email == "" || resp.User.Name == "" {
		t.Errorf("user profile not populated: %+v", resp.User)
	}

	// The access token must be a valid mootd JWT for the mock user.
	claims, err := jwtutil.ValidateToken(resp.AccessToken, authTestSecret)
	if err != nil {
		t.Fatalf("access token failed validation: %v", err)
	}
	if claims.Subject != mockLoginUserID {
		t.Errorf("token sub = %q, want %q", claims.Subject, mockLoginUserID)
	}

	// The raw refresh token returned must hash to the value persisted.
	stored, ok := repo.hashFor(mockLoginUserID)
	if !ok {
		t.Fatal("no refresh token hash persisted for mock user")
	}
	if stored != jwtutil.HashRefreshToken(resp.RefreshToken) {
		t.Error("stored refresh hash does not match returned refresh token")
	}
}

// An empty/default provider is treated as "google"; only an explicitly
// unsupported provider is rejected.
func TestMockLogin_DefaultsToGoogle(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.MockLogin, http.MethodPost, "/v1/auth/mock-login", `{"provider":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestMockLogin_UnsupportedProvider(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.MockLogin, http.MethodPost, "/v1/auth/mock-login", `{"provider":"facebook"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody[map[string]string](t, rec)["error"]; got != "unsupported provider" {
		t.Errorf("error = %q, want %q", got, "unsupported provider")
	}
}

func TestMockLogin_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := doJSON(h.MockLogin, m, "/v1/auth/mock-login", "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", m, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

// seedRefresh persists a valid refresh token for userID (with a 30-day expiry)
// and returns the raw token the client would present.
func seedRefresh(t *testing.T, repo *fakeAuthRepo, userID, email, name string) string {
	t.Helper()
	raw, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}
	repo.users[userID] = &UserDocument{ID: userID, Email: email, Name: name}
	if err := repo.SaveRefreshToken(context.Background(), userID, jwtutil.HashRefreshToken(raw), time.Now().Add(30*24*time.Hour)); err != nil {
		t.Fatalf("save refresh token: %v", err)
	}
	return raw
}

func TestRefresh_RotatesTokenPair(t *testing.T) {
	repo := newFakeAuthRepo()
	const uid, email, name = "user_xyz", "rex@example.com", "Rex"
	oldRaw := seedRefresh(t, repo, uid, email, name)
	oldHash := jwtutil.HashRefreshToken(oldRaw)

	h := newTestHandler(repo)
	rec := doJSON(h.Refresh, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"`+oldRaw+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	resp := decodeBody[RefreshResponse](t, rec)
	if resp.AccessToken == "" {
		t.Error("accessToken is empty")
	}
	if resp.RefreshToken == "" {
		t.Fatal("refreshToken is empty")
	}
	if resp.RefreshToken == oldRaw {
		t.Error("refresh token was not rotated (same raw value returned)")
	}
	if resp.User.ID != uid || resp.User.Email != email || resp.User.Name != name {
		t.Errorf("user echoed = %+v, want id=%q email=%q name=%q", resp.User, uid, email, name)
	}

	// New access token must validate and carry the user's identity.
	claims, err := jwtutil.ValidateToken(resp.AccessToken, authTestSecret)
	if err != nil {
		t.Fatalf("new access token failed validation: %v", err)
	}
	if claims.Subject != uid {
		t.Errorf("token sub = %q, want %q", claims.Subject, uid)
	}

	// The stored hash must now be the NEW token's hash, and the OLD hash must
	// no longer resolve to a user (single-use rotation).
	newHash := jwtutil.HashRefreshToken(resp.RefreshToken)
	stored, ok := repo.hashFor(uid)
	if !ok {
		t.Fatal("no refresh hash stored after rotation")
	}
	if stored != newHash {
		t.Error("stored hash is not the newly issued refresh token's hash")
	}
	if stored == oldHash {
		t.Error("old refresh hash is still the stored hash (not rotated)")
	}
	if got, _ := repo.FindByRefreshToken(context.Background(), oldHash); got != nil {
		t.Error("old refresh token still resolves to a user after rotation")
	}
}

func TestRefresh_UnknownToken(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Refresh, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"does-not-exist"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody[map[string]string](t, rec)["error"]; got != "invalid or expired refresh token" {
		t.Errorf("error = %q, want %q", got, "invalid or expired refresh token")
	}
}

// An expired refresh token must be treated exactly like an unknown one.
func TestRefresh_ExpiredToken(t *testing.T) {
	repo := newFakeAuthRepo()
	raw, err := jwtutil.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}
	repo.users["user_exp"] = &UserDocument{ID: "user_exp"}
	if err := repo.SaveRefreshToken(context.Background(), "user_exp", jwtutil.HashRefreshToken(raw), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("save refresh token: %v", err)
	}

	h := newTestHandler(repo)
	rec := doJSON(h.Refresh, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"`+raw+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestRefresh_MissingToken(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Refresh, http.MethodPost, "/v1/auth/refresh", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody[map[string]string](t, rec)["error"]; got != "refreshToken is required" {
		t.Errorf("error = %q, want %q", got, "refreshToken is required")
	}
}

// A repository failure on lookup must surface as a 500, not a 401 — distinct
// from "token not found" so a transient DB blip doesn't look like a bad token.
func TestRefresh_RepositoryError(t *testing.T) {
	repo := newFakeAuthRepo()
	repo.findErr = errors.New("db down")
	h := newTestHandler(repo)
	rec := doJSON(h.Refresh, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"anything"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestRefresh_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Refresh, http.MethodGet, "/v1/auth/refresh", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

func TestLogout_ClearsMatchingToken(t *testing.T) {
	repo := newFakeAuthRepo()
	raw := seedRefresh(t, repo, "user_lo", "lo@example.com", "Lo")
	if _, ok := repo.hashFor("user_lo"); !ok {
		t.Fatal("precondition: refresh hash should be stored before logout")
	}

	h := newTestHandler(repo)
	rec := doJSON(h.Logout, http.MethodPost, "/v1/auth/logout", `{"refreshToken":"`+raw+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response should have empty body, got %q", rec.Body.String())
	}
	if _, ok := repo.hashFor("user_lo"); ok {
		t.Error("refresh hash was not cleared on logout")
	}
}

// Logout must NOT be a refresh-token oracle: an unknown token still returns 204,
// indistinguishable from a successful revocation.
func TestLogout_UnknownTokenStill204(t *testing.T) {
	repo := newFakeAuthRepo()
	// Seed a DIFFERENT user's token to prove logout of a non-matching token
	// neither errors nor clears the unrelated session.
	other := seedRefresh(t, repo, "user_other", "o@example.com", "Other")
	_ = other

	h := newTestHandler(repo)
	rec := doJSON(h.Logout, http.MethodPost, "/v1/auth/logout", `{"refreshToken":"unknown-token"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", rec.Code, rec.Body.String())
	}
	if _, ok := repo.hashFor("user_other"); !ok {
		t.Error("logout of an unrelated token cleared another user's session")
	}
}

func TestLogout_MissingTokenStill204(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Logout, http.MethodPost, "/v1/auth/logout", `{}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestLogout_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Logout, http.MethodGet, "/v1/auth/logout", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Google (via httptest stub of Google's tokeninfo/userinfo endpoints)
// ---------------------------------------------------------------------------
//
// google_test.go already defines the googleStub helper + mootdClientID const,
// which point the package-level googleTokenInfoURL/googleUserInfoURL vars at a
// local test server. We reuse them here to drive the handler's full
// verify-then-upsert path without touching the network.

func TestGoogle_Success(t *testing.T) {
	const sub, email, name, pic = "google-987", "g@example.com", "Gina", "https://pic"
	googleStub(t,
		http.StatusOK, `{"aud":"`+mootdClientID+`","sub":"`+sub+`","email":"`+email+`"}`,
		http.StatusOK, `{"sub":"`+sub+`","email":"`+email+`","name":"`+name+`","picture":"`+pic+`"}`,
	)

	repo := newFakeAuthRepo()
	h := newTestHandler(repo)
	rec := doJSON(h.Google, http.MethodPost, "/v1/auth/google", `{"accessToken":"valid-google-token"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	resp := decodeBody[GoogleAuthResponse](t, rec)
	if resp.Mode != "api" {
		t.Errorf("mode = %q, want %q", resp.Mode, "api")
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Error("expected both access and refresh tokens")
	}
	// The response must reflect Google-verified data, not client input.
	if resp.User.ID != sub || resp.User.Email != email || resp.User.Name != name || resp.User.AvatarURL != pic {
		t.Errorf("user = %+v, want verified sub=%q email=%q name=%q pic=%q", resp.User, sub, email, name, pic)
	}

	claims, err := jwtutil.ValidateToken(resp.AccessToken, authTestSecret)
	if err != nil {
		t.Fatalf("access token failed validation: %v", err)
	}
	if claims.Subject != sub {
		t.Errorf("token sub = %q, want %q", claims.Subject, sub)
	}

	// Exactly one upsert, carrying only the Google-verified fields.
	if len(repo.upsertCalls) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(repo.upsertCalls))
	}
	if got := repo.upsertCalls[0]; got.googleID != sub || got.email != email || got.name != name || got.avatarURL != pic {
		t.Errorf("upsert call = %+v, want sub=%q email=%q name=%q pic=%q", got, sub, email, name, pic)
	}

	// A refresh token must have been persisted for the verified subject.
	stored, ok := repo.hashFor(sub)
	if !ok {
		t.Fatal("no refresh hash persisted for google user")
	}
	if stored != jwtutil.HashRefreshToken(resp.RefreshToken) {
		t.Error("stored refresh hash does not match returned refresh token")
	}
}

// A valid Google token minted for a DIFFERENT OAuth client (wrong audience)
// must be rejected with 401 and must not create a session.
func TestGoogle_WrongAudienceRejected(t *testing.T) {
	googleStub(t,
		http.StatusOK, `{"aud":"attacker-999.apps.googleusercontent.com","sub":"victim-1","email":"victim@example.com"}`,
		http.StatusOK, `{"sub":"victim-1","email":"victim@example.com","name":"Victim"}`,
	)

	repo := newFakeAuthRepo()
	h := newTestHandler(repo)
	rec := doJSON(h.Google, http.MethodPost, "/v1/auth/google", `{"accessToken":"foreign-token"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody[map[string]string](t, rec)["error"]; got != "invalid Google token" {
		t.Errorf("error = %q, want %q", got, "invalid Google token")
	}
	if len(repo.upsertCalls) != 0 {
		t.Errorf("a rejected token must not upsert a user (got %d upserts)", len(repo.upsertCalls))
	}
}

// An invalid/expired token (Google's tokeninfo returns non-200) must 401.
func TestGoogle_InvalidTokenRejected(t *testing.T) {
	googleStub(t,
		http.StatusBadRequest, `{"error":"invalid_token"}`,
		http.StatusOK, `{"sub":"x","email":"x@example.com"}`,
	)

	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Google, http.MethodPost, "/v1/auth/google", `{"accessToken":"expired"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestGoogle_MissingAccessToken(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Google, http.MethodPost, "/v1/auth/google", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody[map[string]string](t, rec)["error"]; got != "accessToken is required" {
		t.Errorf("error = %q, want %q", got, "accessToken is required")
	}
}

func TestGoogle_MethodNotAllowed(t *testing.T) {
	h := newTestHandler(newFakeAuthRepo())
	rec := doJSON(h.Google, http.MethodGet, "/v1/auth/google", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// Compile-time assertion that the fake satisfies the interface the handler
// depends on.
var _ Repository = (*fakeAuthRepo)(nil)
