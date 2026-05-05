package outfit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// wantsSSE is the smallest piece of the SSE branch to gate
// directly. The rest of the streaming path needs a service +
// generator + Redis stack to test end-to-end and is exercised
// by the docker-compose integration smoke test instead.
func TestWantsSSE_AcceptHeaderParsing(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"*/*", false},
		{"application/json", false},
		{"text/event-stream", true},
		{"text/event-stream; charset=utf-8", true},
		{"application/json, text/event-stream", true},
		{"text/event-stream;q=0.9, application/json;q=1.0", true},
		// Substring match must NOT trigger — the parser splits
		// on commas and trims so we don't false-positive on
		// "application/text/event-stream-ish" or other oddities.
		{"text/event-streamish", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		if c.accept != "" {
			req.Header.Set("Accept", c.accept)
		}
		got := wantsSSE(req)
		if got != c.want {
			t.Errorf("wantsSSE(Accept=%q) = %v, want %v", c.accept, got, c.want)
		}
	}
}
