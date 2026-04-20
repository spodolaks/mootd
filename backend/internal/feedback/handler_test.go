package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mootd/backend/internal/shared/middleware"
)

// fakeRepo is an in-memory Repository for handler tests.
type fakeRepo struct {
	events    []Event
	insertErr error
}

func (f *fakeRepo) Insert(_ context.Context, e Event) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakeRepo) ListByUser(_ context.Context, userID string, _ int) ([]Event, error) {
	var out []Event
	for _, e := range f.events {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeRepo) DeleteAllByUser(_ context.Context, userID string) (int, error) {
	kept := f.events[:0]
	removed := 0
	for _, e := range f.events {
		if e.UserID == userID {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	f.events = kept
	return removed, nil
}

// newAuthedRequest stamps the user ID into the request context the same way
// middleware.Auth would.
func newAuthedRequest(t *testing.T, body string, userID string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/outfits/feedback", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	return r.WithContext(ctx)
}

func TestSubmit_AcceptsSavedEvent(t *testing.T) {
	repo := &fakeRepo{}
	h := NewHandler(log.New(io.Discard, "", 0), repo)

	body := `{"action":"saved","chosenOutfitId":"o1","jobId":"j1","generatedBatch":[{"id":"o1","items":["i1","i2"]}]}`
	req := newAuthedRequest(t, body, "user_abc")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.events) != 1 {
		t.Fatalf("inserted events = %d, want 1", len(repo.events))
	}
	got := repo.events[0]
	if got.UserID != "user_abc" {
		t.Errorf("userID = %q, want user_abc (must come from context, not body)", got.UserID)
	}
	if got.Action != ActionSaved || got.ChosenOutfitID != "o1" {
		t.Errorf("unexpected event: %+v", got)
	}
	if got.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", got.SchemaVersion, CurrentSchemaVersion)
	}
	if got.ID == "" || got.CreatedAt.IsZero() {
		t.Errorf("id/createdAt should be populated: %+v", got)
	}

	var resp SubmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Errorf("response body = %s, err=%v", rec.Body.String(), err)
	}
}

func TestSubmit_RejectsUnknownAction(t *testing.T) {
	repo := &fakeRepo{}
	h := NewHandler(log.New(io.Discard, "", 0), repo)

	req := newAuthedRequest(t, `{"action":"bogus"}`, "u1")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if len(repo.events) != 0 {
		t.Errorf("expected no insert, got %d", len(repo.events))
	}
}

func TestSubmit_RatedRequiresValidRating(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing", `{"action":"rated"}`, http.StatusBadRequest},
		{"too low", `{"action":"rated","rating":0}`, http.StatusBadRequest},
		{"too high", `{"action":"rated","rating":6}`, http.StatusBadRequest},
		{"valid", `{"action":"rated","rating":4}`, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}
			h := NewHandler(log.New(io.Discard, "", 0), repo)
			req := newAuthedRequest(t, tc.body, "u1")
			rec := httptest.NewRecorder()
			h.Submit(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestSubmit_RequiresAuth(t *testing.T) {
	repo := &fakeRepo{}
	h := NewHandler(log.New(io.Discard, "", 0), repo)

	// No UserIDKey in context.
	req := httptest.NewRequest(http.MethodPost, "/v1/outfits/feedback",
		bytes.NewBufferString(`{"action":"saved"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestSubmit_InsertFailurePropagates500(t *testing.T) {
	repo := &fakeRepo{insertErr: errors.New("mongo down")}
	h := NewHandler(log.New(io.Discard, "", 0), repo)

	req := newAuthedRequest(t, `{"action":"saved"}`, "u1")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestSubmit_PersistsSwapMetadata(t *testing.T) {
	repo := &fakeRepo{}
	h := NewHandler(log.New(io.Discard, "", 0), repo)

	body := `{"action":"item_swapped","chosenOutfitId":"o1","swappedFrom":"item_a","swappedTo":"item_b"}`
	req := newAuthedRequest(t, body, "u1")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.events) != 1 {
		t.Fatalf("events = %d, want 1", len(repo.events))
	}
	got := repo.events[0]
	if got.SwappedFrom != "item_a" || got.SwappedTo != "item_b" {
		t.Errorf("swap metadata = (%q, %q), want (item_a, item_b)", got.SwappedFrom, got.SwappedTo)
	}
}

func TestSubmit_IgnoresUserIDInBody(t *testing.T) {
	// SubmitRequest has no UserID field, but a client might try sending one.
	// Verify the handler still attributes the event to the JWT user.
	repo := &fakeRepo{}
	h := NewHandler(log.New(io.Discard, "", 0), repo)

	req := newAuthedRequest(t, `{"action":"saved","userId":"victim"}`, "caller")
	rec := httptest.NewRecorder()
	h.Submit(rec, req)

	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	// Some DecodeJSONBody implementations reject unknown fields (400). If it
	// accepted, the event must still carry the JWT user.
	if rec.Code == http.StatusCreated {
		if len(repo.events) != 1 || repo.events[0].UserID != "caller" {
			t.Errorf("userID spoofed: %+v", repo.events)
		}
	}
}
