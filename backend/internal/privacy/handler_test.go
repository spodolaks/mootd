package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"mootd/backend/internal/shared/middleware"
)

// fakePurger is a test double for the purgeExporter seam so the handler can be
// exercised without a live Mongo.
type fakePurger struct {
	report       *PurgeReport
	purgeErr     error
	export       *ExportData
	exportErr    error
	gotUser      string
	ctxErrAtCall error // ctx.Err() observed inside Purge — nil means not cancelled
	called       bool
}

func (f *fakePurger) Purge(ctx context.Context, userID string) (*PurgeReport, error) {
	f.called = true
	f.gotUser = userID
	f.ctxErrAtCall = ctx.Err()
	return f.report, f.purgeErr
}

func (f *fakePurger) Export(ctx context.Context, userID string) (*ExportData, error) {
	f.gotUser = userID
	return f.export, f.exportErr
}

func authedDelete(userID string) *http.Request {
	r := httptest.NewRequest(http.MethodDelete, "/v1/privacy/self", nil)
	ctx := context.WithValue(r.Context(), middleware.UserIDKey, userID)
	return r.WithContext(ctx)
}

func newTestHandler(svc purgeExporter) *Handler {
	return NewHandler(log.New(io.Discard, "", 0), svc)
}

func TestSelfPurge_Success(t *testing.T) {
	f := &fakePurger{report: &PurgeReport{UserID: "u1", Total: 3, Collections: map[string]int64{"wardrobe_items": 3}}}
	rec := httptest.NewRecorder()
	newTestHandler(f).SelfPurge(rec, authedDelete("u1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if f.gotUser != "u1" {
		t.Errorf("purge user = %q, want u1", f.gotUser)
	}
	var got PurgeReport
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if got.Total != 3 {
		t.Errorf("report total = %d, want 3", got.Total)
	}
}

func TestSelfPurge_AlreadyPurgedReturns404(t *testing.T) {
	f := &fakePurger{purgeErr: ErrUserNotFound}
	rec := httptest.NewRecorder()
	newTestHandler(f).SelfPurge(rec, authedDelete("u1"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSelfPurge_InternalErrorReturns500(t *testing.T) {
	f := &fakePurger{purgeErr: errors.New("boom")}
	rec := httptest.NewRecorder()
	newTestHandler(f).SelfPurge(rec, authedDelete("u1"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestSelfPurge_Unauthorized(t *testing.T) {
	f := &fakePurger{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/privacy/self", nil) // no UserIDKey
	newTestHandler(f).SelfPurge(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if f.called {
		t.Error("purge ran for an unauthenticated request")
	}
}

// TestSelfPurge_DecoupledFromRequestCancellation proves the #96 fix: erasure
// must complete even if the client disconnects. We cancel the request context
// up front; the purge must still run on a live (non-cancelled) context.
func TestSelfPurge_DecoupledFromRequestCancellation(t *testing.T) {
	f := &fakePurger{report: &PurgeReport{UserID: "u1", Collections: map[string]int64{}}}
	req := authedDelete("u1")
	ctx, cancel := context.WithCancel(req.Context())
	cancel() // client "disconnects" before the handler runs
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	newTestHandler(f).SelfPurge(rec, req)

	if !f.called {
		t.Fatal("purge was not called")
	}
	if f.ctxErrAtCall != nil {
		t.Errorf("purge ran on a cancelled context (%v); erasure must be decoupled from the request (#96)", f.ctxErrAtCall)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
