package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqWith(remoteAddr, xff string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP_HonorsXFFFromTrustedPeer(t *testing.T) {
	// Default trusted set is loopback.
	got := clientIP(reqWith("127.0.0.1:9999", "203.0.113.7, 127.0.0.1"))
	if got == nil || got.String() != "203.0.113.7" {
		t.Fatalf("trusted peer: want 203.0.113.7, got %v", got)
	}
}

func TestClientIP_IgnoresXFFFromUntrustedPeer(t *testing.T) {
	// Direct caller from a public IP forging XFF — header must be ignored.
	got := clientIP(reqWith("198.51.100.9:5555", "10.0.0.1"))
	if got == nil || got.String() != "198.51.100.9" {
		t.Fatalf("untrusted peer: want 198.51.100.9 (RemoteAddr), got %v", got)
	}
}

func TestClientIP_FallsBackToPeerWhenNoXFF(t *testing.T) {
	got := clientIP(reqWith("127.0.0.1:1111", ""))
	if got == nil || got.String() != "127.0.0.1" {
		t.Fatalf("no XFF: want 127.0.0.1, got %v", got)
	}
}

func TestSetTrustedProxies_CustomThenReset(t *testing.T) {
	// Restore the default loopback set for other tests in this package.
	defer SetTrustedProxies(nil, silentLogger{})

	SetTrustedProxies([]string{"203.0.113.0/24"}, silentLogger{})

	// Loopback is no longer trusted under the custom set.
	if got := clientIP(reqWith("127.0.0.1:1", "10.0.0.1")); got.String() != "127.0.0.1" {
		t.Errorf("loopback should be untrusted now: got %v", got)
	}
	// The configured proxy range is trusted.
	if got := clientIP(reqWith("203.0.113.5:1", "10.0.0.1")); got.String() != "10.0.0.1" {
		t.Errorf("configured proxy should be trusted: got %v", got)
	}
}

func TestRateLimitKey_IgnoresSpoofedXFF(t *testing.T) {
	// Two requests from the same untrusted peer with different forged XFF
	// values must collapse to the SAME key (peer IP) — otherwise the
	// limiter is bypassable by rotating the header.
	k1 := rateLimitKey(reqWith("198.51.100.9:1", "1.1.1.1"))
	k2 := rateLimitKey(reqWith("198.51.100.9:2", "2.2.2.2"))
	if k1 != k2 {
		t.Errorf("spoofed XFF produced distinct keys %q vs %q (limiter bypassable)", k1, k2)
	}
	if k1 != "ip:198.51.100.9" {
		t.Errorf("want key ip:198.51.100.9, got %q", k1)
	}
}
