package events

import "testing"

func TestValidateEvent_HappyPath(t *testing.T) {
	for name := range CatalogNames {
		if reason := validateEvent(IngestEvent{
			Name:      name,
			SessionID: "sess-1",
		}); reason != "" {
			t.Errorf("catalog name %q rejected: %s", name, reason)
		}
	}
}

func TestValidateEvent_RejectsUnknownName(t *testing.T) {
	cases := []string{"", "hacked_event", "ScreenView", "screen-view"}
	for _, n := range cases {
		ev := IngestEvent{Name: n, SessionID: "sess-1"}
		if validateEvent(ev) == "" {
			t.Errorf("expected reject for name %q", n)
		}
	}
}

func TestValidateEvent_RequiresSessionID(t *testing.T) {
	if validateEvent(IngestEvent{Name: "app_opened", SessionID: ""}) == "" {
		t.Error("expected reject for missing sessionId")
	}
}

func TestCatalogNames_HasCoreEvents(t *testing.T) {
	// Spot-check the events Phase 2's analyses lean on. If any
	// of these go missing, downstream funnel + retention queries
	// silently return empties — fail loudly here instead.
	mustHave := []string{
		"app_opened", "session_start", "session_end",
		"signed_up", "signed_in", "signed_out",
		"photo_uploaded", "items_detected",
		"generated_outfit", "saved_moodboard",
	}
	for _, n := range mustHave {
		if !CatalogNames[n] {
			t.Errorf("catalog missing %q", n)
		}
	}
}
