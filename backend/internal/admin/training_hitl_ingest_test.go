package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeOrchestrator serves the two endpoints the HITL patch flow hits:
// GET item (pre-edit snapshot) and PATCH attributes (the forward).
func fakeOrchestrator(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/admin/items/"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"requestId": "abc",
				"item": map[string]any{
					"imageUrl":              "gridfs://sid_uploads/0123456789abcdef01234567",
					"structuredDescription": map[string]any{"color": map[string]any{"primary": "blue"}, "fit": "slim"},
				},
			})
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/attributes"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestHitlPatchAttributes_IngestsTrainingPair(t *testing.T) {
	srv := fakeOrchestrator(t)
	defer srv.Close()

	store := newMemTrainingTrials()
	h := trainingTestHandler(store)
	h.WithHitlProxy(&HitlProxy{BaseURL: srv.URL, APIKey: "k", Client: &http.Client{Timeout: 5 * time.Second}})

	rr := httptest.NewRecorder()
	req := trainingReq(http.MethodPatch, "/admin/v1/items/abc/attributes",
		`{"patches":{"color.primary":"navy"},"reason":"fix"}`, RoleEngineer)
	h.HitlItemsRouter(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("patch: want 204 from forward, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Collect the ingested record.
	var got []TrainingTrial
	_, err := store.StreamSubmittedTrainingTrials(context.Background(), time.Time{}, 0, func(tr TrainingTrial) error {
		got = append(got, tr)
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 ingested record, got %d", len(got))
	}
	rec := got[0]
	if rec.Source != TrainingSourceHITL || !strings.HasPrefix(rec.ID, "hitl-abc-") {
		t.Fatalf("unexpected record id/source: %s / %s", rec.ID, rec.Source)
	}
	// Gemma = rejected (original), Claude = chosen (corrected).
	if flattenDesc(normalizeDesc(rec.GemmaDescription))["color.primary"] != "blue" {
		t.Fatalf("rejected (gemma) should keep original blue: %+v", rec.GemmaDescription)
	}
	if flattenDesc(normalizeDesc(rec.ClaudeDescription))["color.primary"] != "navy" {
		t.Fatalf("chosen (claude) should be navy: %+v", rec.ClaudeDescription)
	}
	if rec.Picks["color.primary"] != "claude" {
		t.Fatalf("patched path should be a claude pick: %+v", rec.Picks)
	}

	// Excluded from the manual review list…
	lr := httptest.NewRecorder()
	h.TrainingRouter(lr, trainingReq(http.MethodGet, "/admin/v1/training/submissions", "", RoleEngineer))
	var page TrainingTrialPage
	_ = json.Unmarshal(lr.Body.Bytes(), &page)
	if len(page.Trials) != 0 {
		t.Fatalf("HITL record must not show in the manual list, got %d", len(page.Trials))
	}

	// …but present in a DPO export (it's a real correction).
	er := httptest.NewRecorder()
	h.TrainingRouter(er, trainingReq(http.MethodGet, "/admin/v1/training/export?format=dpo", "", RoleEngineer))
	if er.Code != http.StatusOK {
		t.Fatalf("export: want 200, got %d", er.Code)
	}
	dpo := decodeJSONL(t, er.Body.String())
	if len(dpo) != 1 {
		t.Fatalf("dpo export: want the HITL pair, got %d records", len(dpo))
	}
}

// When the training store is unwired, a HITL patch must still forward
// cleanly and skip ingest (no GET-before, no record).
func TestHitlPatchAttributes_NoIngestWhenStoreUnwired(t *testing.T) {
	srv := fakeOrchestrator(t)
	defer srv.Close()

	h := trainingTestHandler(nil) // nil → training store unwired
	h.WithHitlProxy(&HitlProxy{BaseURL: srv.URL, APIKey: "k", Client: &http.Client{Timeout: 5 * time.Second}})

	rr := httptest.NewRecorder()
	req := trainingReq(http.MethodPatch, "/admin/v1/items/abc/attributes",
		`{"patches":{"color.primary":"navy"}}`, RoleEngineer)
	h.HitlItemsRouter(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("patch with no training store: want 204, got %d", rr.Code)
	}
}
