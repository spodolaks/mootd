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

func TestReconstructGold(t *testing.T) {
	base := TrainingTrial{
		ClaudeDescription: map[string]any{"color": map[string]any{"primary": "navy"}, "fit": "slim"},
		GemmaDescription:  map[string]any{"color": map[string]any{"primary": "blue"}, "fit": "slim"},
	}

	t.Run("pick claude + custom changes the gold", func(t *testing.T) {
		tr := base
		tr.Picks = map[string]string{"color.primary": "claude", "fit": "custom"}
		tr.CustomValues = map[string]string{"fit": "relaxed"}
		gold, changed := reconstructGold(tr)
		if !changed {
			t.Fatal("want changed=true")
		}
		flat := flattenDesc(gold)
		if flat["color.primary"] != "navy" {
			t.Fatalf("color.primary: want navy (claude), got %v", flat["color.primary"])
		}
		if flat["fit"] != "relaxed" {
			t.Fatalf("fit: want relaxed (custom), got %v", flat["fit"])
		}
	})

	t.Run("gemma-only picks leave gold == gemma (no signal)", func(t *testing.T) {
		tr := base
		tr.Picks = map[string]string{"color.primary": "gemma"}
		gold, changed := reconstructGold(tr)
		if changed {
			t.Fatal("want changed=false when every pick is gemma")
		}
		if flattenDesc(gold)["color.primary"] != "blue" {
			t.Fatal("gold should keep Gemma's value")
		}
	})

	t.Run("no picks leaves gold == gemma", func(t *testing.T) {
		gold, changed := reconstructGold(base)
		if changed {
			t.Fatal("want changed=false with no picks")
		}
		if flattenDesc(gold)["fit"] != "slim" {
			t.Fatal("gold should equal Gemma")
		}
	})
}

// seedSubmitted writes a submitted trial with a snapshot straight into
// the store, bypassing the HTTP submit (which is covered elsewhere).
func seedSubmitted(t *testing.T, store *memTrainingTrials, id string, picks map[string]string) {
	t.Helper()
	at := time.Now().UTC()
	_, err := store.SubmitTrainingTrial(context.Background(), id, "admin-1", TrainingSubmitInput{
		Picks:             picks,
		AttrCount:         2,
		ClaudeDescription: map[string]any{"color": map[string]any{"primary": "navy"}, "fit": "slim"},
		GemmaDescription:  map[string]any{"color": map[string]any{"primary": "blue"}, "fit": "slim"},
		SourceImageURL:    "gridfs://sid_uploads/0123456789abcdef01234567",
	}, at)
	if err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func decodeJSONL(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad JSONL line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func TestTrainingExport_PermissionAndFormat(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)

	// readonly has traces:read (route floor) but not training:export.
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/export?format=sft", "", RoleReadonly))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("readonly export: want 403, got %d", rr.Code)
	}

	// engineer, bad format → 400.
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/export?format=bogus", "", RoleEngineer))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad format: want 400, got %d", rr.Code)
	}
}

func TestTrainingExport_SFTAndDPO(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)

	seedSubmitted(t, store, "trial-a", map[string]string{"color.primary": "claude"}) // changed → DPO-eligible
	seedSubmitted(t, store, "trial-b", map[string]string{"color.primary": "gemma"})  // gemma won → no DPO signal

	// SFT: one record per submitted trial (both), gold reflects picks.
	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/export?format=sft", "", RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("sft export: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/x-ndjson") {
		t.Fatalf("sft content-type: got %q", ct)
	}
	sft := decodeJSONL(t, rr.Body.String())
	if len(sft) != 2 {
		t.Fatalf("sft: want 2 records, got %d", len(sft))
	}
	// trial-a gold should carry Claude's navy (sort: a before b).
	gold, _ := sft[0]["gold"].(map[string]any)
	color, _ := gold["color"].(map[string]any)
	if color["primary"] != "navy" {
		t.Fatalf("sft trial-a gold color.primary: want navy, got %v", color["primary"])
	}

	// DPO: only trial-a (trial-b has no preference signal → skipped).
	rr = httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/export?format=dpo", "", RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("dpo export: want 200, got %d", rr.Code)
	}
	dpo := decodeJSONL(t, rr.Body.String())
	if len(dpo) != 1 {
		t.Fatalf("dpo: want 1 record (trial-b skipped), got %d", len(dpo))
	}
	if dpo[0]["trialId"] != "trial-a" {
		t.Fatalf("dpo: want trial-a, got %v", dpo[0]["trialId"])
	}
	chosen, _ := dpo[0]["chosen"].(map[string]any)
	rejected, _ := dpo[0]["rejected"].(map[string]any)
	cc, _ := chosen["color"].(map[string]any)
	rc, _ := rejected["color"].(map[string]any)
	if cc["primary"] != "navy" || rc["primary"] != "blue" {
		t.Fatalf("dpo pair: chosen=%v rejected=%v (want navy / blue)", cc["primary"], rc["primary"])
	}
}

func TestTrainingExport_SkipsPreSnapshotTrials(t *testing.T) {
	store := newMemTrainingTrials()
	h := trainingTestHandler(store)

	// A submitted trial with NO snapshot (pre-Phase-1) — not exportable.
	at := time.Now().UTC()
	_, _ = store.SubmitTrainingTrial(context.Background(), "trial-old", "admin-1", TrainingSubmitInput{
		Picks:     map[string]string{"fit": "claude"},
		AttrCount: 1,
	}, at)

	rr := httptest.NewRecorder()
	h.TrainingRouter(rr, trainingReq(http.MethodGet, "/admin/v1/training/export?format=sft", "", RoleEngineer))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if body := strings.TrimSpace(rr.Body.String()); body != "" {
		t.Fatalf("pre-snapshot trial should be skipped, got body %q", body)
	}
}
