package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPickAgreement(t *testing.T) {
	cases := []struct {
		name           string
		pa, ca, pb, cb map[string]string
		want           float64
	}{
		{"identical", map[string]string{"a": "claude", "b": "gemma"}, nil, map[string]string{"a": "claude", "b": "gemma"}, nil, 1},
		{"half", map[string]string{"a": "claude", "b": "gemma"}, nil, map[string]string{"a": "claude", "b": "claude"}, nil, 0.5},
		{"custom value differs", map[string]string{"a": "custom"}, map[string]string{"a": "x"}, map[string]string{"a": "custom"}, map[string]string{"a": "y"}, 0},
		{"custom value same", map[string]string{"a": "custom"}, map[string]string{"a": "x"}, map[string]string{"a": "custom"}, map[string]string{"a": "x"}, 1},
		{"disjoint paths", map[string]string{"a": "claude"}, nil, map[string]string{"b": "claude"}, nil, 0},
		{"both empty", nil, nil, nil, nil, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pickAgreement(c.pa, c.ca, c.pb, c.cb); got != c.want {
				t.Fatalf("pickAgreement = %v, want %v", got, c.want)
			}
		})
	}
}

// trainingReqAs is trainingReq with a caller-chosen admin id (the
// built-in helper hardcodes admin-1; re-review needs two identities).
func trainingReqAs(adminID, method, target, body string, roles ...Role) *http.Request {
	r := trainingReq(method, target, body, roles...)
	rs := make([]string, len(roles))
	for i, role := range roles {
		rs[i] = string(role)
	}
	return r.WithContext(ContextWithAuth(r.Context(), adminID, rs, true))
}

func TestSubmitTrainingTrial_ReReviewRecordsAgreement(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)

	// First review by admin-1.
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReqAs("admin-1", http.MethodPost, "/admin/v1/training/submissions/trial-x/submit",
		`{"picks":{"color.primary":"claude","fit":"gemma"},"attrCount":2,"gemmaDescription":{"fit":"slim"}}`, RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("first submit: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Second review by admin-2: agrees on color.primary, differs on fit → 0.5.
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReqAs("admin-2", http.MethodPost, "/admin/v1/training/submissions/trial-x/submit",
		`{"picks":{"color.primary":"claude","fit":"claude"},"attrCount":2}`, RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("re-review: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	rec, _ := store.GetTrainingTrial(context.Background(), "trial-x")
	if rec == nil || rec.ReviewCount != 2 {
		t.Fatalf("want reviewCount 2, got %+v", rec)
	}
	if rec.Agreement == nil || *rec.Agreement != 0.5 {
		t.Fatalf("want agreement 0.5, got %v", rec.Agreement)
	}
	if rec.SecondReviewer != "admin-2" {
		t.Fatalf("want secondReviewer admin-2, got %q", rec.SecondReviewer)
	}
	// First review stays canonical: picks + submittedBy unchanged.
	if rec.SubmittedBy != "admin-1" || rec.Picks["fit"] != "gemma" {
		t.Fatalf("first review must stay canonical, got submittedBy=%q picks=%+v", rec.SubmittedBy, rec.Picks)
	}

	// Same admin re-submitting is a normal edit, not a re-review.
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReqAs("admin-1", http.MethodPost, "/admin/v1/training/submissions/trial-x/submit",
		`{"picks":{"color.primary":"gemma"},"attrCount":2}`, RoleEngineer))
	rec, _ = store.GetTrainingTrial(context.Background(), "trial-x")
	if rec.Picks["color.primary"] != "gemma" {
		t.Fatalf("same-admin re-submit should overwrite picks, got %+v", rec.Picks)
	}
}

func TestTrainingExport_MinAgreementThreshold(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)

	// Dual-reviewed trial, agreement 0.5, with a correction (DPO-eligible).
	seedSubmitted(t, store, "trial-dual", map[string]string{"color.primary": "claude"})
	if _, err := store.RecordReReview(context.Background(), "trial-dual", "admin-2", 0.5, time.Now().UTC()); err != nil {
		t.Fatalf("re-review seed: %v", err)
	}
	// Single-reviewed trial (no agreement score).
	seedSubmitted(t, store, "trial-single", map[string]string{"color.primary": "claude"})

	get := func(qs string) []map[string]any {
		rr := httptest.NewRecorder()
		h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/export?"+qs, "", RoleEngineer))
		if rr.Code != http.StatusOK {
			t.Fatalf("export %s: want 200, got %d", qs, rr.Code)
		}
		return decodeJSONL(t, rr.Body.String())
	}

	// No threshold → both dual + single emitted.
	if n := len(get("format=dpo")); n != 2 {
		t.Fatalf("no threshold: want 2, got %d", n)
	}
	// 0.8 bar → dual (0.5) fails, single (no score) fails → 0.
	if n := len(get("format=dpo&minAgreement=0.8")); n != 0 {
		t.Fatalf("0.8 bar: want 0, got %d", n)
	}
	// 0.4 bar → dual (0.5) passes; single still excluded (no score) → 1.
	rows := get("format=dpo&minAgreement=0.4")
	if len(rows) != 1 || rows[0]["trialId"] != "trial-dual" {
		t.Fatalf("0.4 bar: want only trial-dual, got %+v", rows)
	}

	// Bad threshold → 400.
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/export?format=dpo&minAgreement=2", "", RoleEngineer))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("minAgreement=2: want 400, got %d", rr.Code)
	}
}

// The needsRereview queue returns only single-opinion submitted trials
// the caller did not submit (mootd-admin#127).
func TestListTrainingTrials_NeedsRereviewQueue(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)
	ctx := context.Background()
	now := time.Now().UTC()
	submit := func(id, by string) {
		if _, err := store.SubmitTrainingTrial(ctx, id, by,
			TrainingSubmitInput{Picks: map[string]string{"fit": "claude"}, AttrCount: 1}, now); err != nil {
			t.Fatalf("seed submit %s: %v", id, err)
		}
	}
	submit("trial-a", "admin-2") // someone else's single opinion → in queue
	submit("trial-b", "admin-1") // caller's own review → excluded
	submit("trial-c", "admin-2") // re-reviewed below → excluded
	if _, err := store.RecordReReview(ctx, "trial-c", "admin-1", 0.9, now); err != nil {
		t.Fatalf("seed re-review: %v", err)
	}
	// Still in_review → excluded (only submitted trials are re-reviewable).
	if err := store.CreateTrainingTrial(ctx, TrainingTrial{
		ID: "trial-d", Status: TrainingStatusInReview, CreatedBy: "admin-2", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed in_review: %v", err)
	}

	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReqAs("admin-1", http.MethodGet,
		"/admin/v1/training/submissions?needsRereview=true", "", RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var page TrainingTrialPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Trials) != 1 || page.Trials[0].ID != "trial-a" {
		t.Fatalf("re-review queue: want only [trial-a], got %+v", page.Trials)
	}
}

// agreement should round-trip through the JSON response on GET.
func TestTrainingTrial_AgreementSerialises(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)
	seedSubmitted(t, store, "trial-j", map[string]string{"fit": "claude"})
	_, _ = store.RecordReReview(context.Background(), "trial-j", "admin-2", 0.75, time.Now().UTC())

	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/submissions/trial-j", "", RoleEngineer))
	var rec TrainingTrial
	if err := json.Unmarshal(rr.Body.Bytes(), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Agreement == nil || *rec.Agreement != 0.75 || rec.ReviewCount != 2 {
		t.Fatalf("agreement not serialised: %+v", rec)
	}
}
