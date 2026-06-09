package wardrobe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1", // loopback
		"10.0.0.1", "172.16.5.4", "192.168.1.1", "fd00::1", // private / IPv6 ULA
		"169.254.169.254", "fe80::1", // link-local (incl. cloud-metadata endpoint)
		"0.0.0.0", "::", // unspecified
		"224.0.0.1", // multicast
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q is not a valid IP", s)
		}
		if !blockedIP(ip) {
			t.Errorf("blockedIP(%s) = false, want true", s)
		}
	}

	public := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"}
	for _, s := range public {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: %q is not a valid IP", s)
		}
		if blockedIP(ip) {
			t.Errorf("blockedIP(%s) = true, want false", s)
		}
	}
}

// TestImageDownloadClientBlocksLoopback proves the dialer Control hook refuses a
// real connection to a private IP — the DNS-rebinding/redirect-proof core of the
// SSRF guard — even when the URL string is handed straight to the client.
// httptest binds to 127.0.0.1, which the guard must reject at dial time.
func TestImageDownloadClientBlocksLoopback(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		_, _ = w.Write([]byte("should never be reached"))
	}))
	defer srv.Close()

	_, _, err := downloadImage(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("downloadImage to loopback returned nil error; SSRF guard did not fire")
	}
	if !strings.Contains(err.Error(), "ssrf guard") {
		t.Errorf("error = %q, want it to mention the ssrf guard", err)
	}
	if reached {
		t.Error("request reached the loopback server; SSRF guard failed to block the dial")
	}
}
