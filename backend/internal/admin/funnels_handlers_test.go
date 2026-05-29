package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeFunnels is an in-memory FunnelsRepository for handler tests.
type fakeFunnels struct {
	created []Funnel
}

func (f *fakeFunnels) List(ctx context.Context) ([]Funnel, error) { return f.created, nil }
func (f *fakeFunnels) Get(ctx context.Context, id string) (*Funnel, error) {
	for i := range f.created {
		if f.created[i].ID == id {
			return &f.created[i], nil
		}
	}
	return nil, nil
}
func (f *fakeFunnels) Create(ctx context.Context, fn Funnel) (Funnel, error) {
	if fn.ID == "" {
		fn.ID = fmt.Sprintf("fn_test_%d", len(f.created))
	}
	f.created = append(f.created, fn)
	return fn, nil
}
func (f *fakeFunnels) Stats(ctx context.Context, id string) (*FunnelStats, error) { return nil, nil }

// #111 F1/F7/F8: createFunnel must default a missing window to 7/30,
// echo the persisted row WITH its generated id (not a name-scan), and
// write a funnel.create audit row.
func TestCreateFunnel_AuditsDefaultsAndEchoesID(t *testing.T) {
	h, repo := newTestHandler(t)
	ff := &fakeFunnels{}
	h.WithFunnels(ff)

	body := `{"name":"Onboarding","steps":[{"eventName":"signed_up"},{"eventName":"photo_uploaded"}]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/funnels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuth(req.Context(), "adm_1", []string{"admin"}, true))
	rec := httptest.NewRecorder()

	h.createFunnel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body)
	}
	var got Funnel
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID == "" {
		t.Error("F7: created funnel should echo its generated ID")
	}
	if got.WindowDays != 7 || got.AnalysisDays != 30 {
		t.Errorf("F8: expected defaults 7/30, got %d/%d", got.WindowDays, got.AnalysisDays)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	found := false
	for _, a := range repo.audits {
		if a.Action == "funnel.create" {
			found = true
			if a.TargetEntity != got.ID {
				t.Errorf("audit TargetEntity = %q, want funnel id %q", a.TargetEntity, got.ID)
			}
		}
	}
	if !found {
		t.Error("F1: expected a funnel.create audit entry to be written")
	}
}

func TestCreateFunnel_NegativeWindowGetsDefault(t *testing.T) {
	h, _ := newTestHandler(t)
	h.WithFunnels(&fakeFunnels{})

	// F8: a negative window (not just 0) must fall back to the default
	// rather than reaching the repo and 400ing.
	body := `{"name":"X","windowDays":-5,"analysisDays":-1,"steps":[{"eventName":"signed_up"},{"eventName":"photo_uploaded"}]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/funnels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuth(req.Context(), "adm_1", []string{"admin"}, true))
	rec := httptest.NewRecorder()

	h.createFunnel(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body)
	}
	var got Funnel
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.WindowDays != 7 || got.AnalysisDays != 30 {
		t.Errorf("expected negative inputs to default to 7/30, got %d/%d", got.WindowDays, got.AnalysisDays)
	}
}
