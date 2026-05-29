package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// trustedProxies is the set of immediate-peer (RemoteAddr) CIDRs whose
// X-Forwarded-For header we are willing to trust. Configured once at
// startup via SetTrustedProxies. It defaults to loopback because the
// documented deployment binds the backend to 127.0.0.1 behind a
// same-host Caddy — so the only legitimate peer is the local proxy.
var (
	trustedProxiesMu sync.RWMutex
	trustedProxies   = defaultTrustedProxies()
)

// defaultTrustedProxies returns the loopback CIDRs trusted when nothing
// is configured. Backend-behind-local-Caddy is the supported topology.
func defaultTrustedProxies() []*net.IPNet {
	out := make([]*net.IPNet, 0, 2)
	for _, c := range []string{"127.0.0.0/8", "::1/128"} {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// SetTrustedProxies configures which immediate peers (the TCP RemoteAddr)
// count as trusted reverse proxies. X-Forwarded-For is honored ONLY when
// the peer is in this set; for any other (direct) connection the header
// is treated as attacker-controlled and ignored. An empty/nil list resets
// to the loopback default. Call once at startup before serving requests.
//
// Set TRUSTED_PROXY_CIDRS to the proxy/edge ranges that actually sit in
// front of the backend (e.g. add Cloudflare's ranges if it terminates the
// connection to this process rather than Caddy).
func SetTrustedProxies(cidrs []string, logger interface{ Printf(string, ...any) }) {
	if logger == nil {
		logger = noopLogger{}
	}
	nets := parseCIDRs(cidrs, logger)
	trustedProxiesMu.Lock()
	defer trustedProxiesMu.Unlock()
	if len(nets) == 0 {
		trustedProxies = defaultTrustedProxies()
		logger.Printf("middleware: trusted proxies defaulted to loopback (127.0.0.0/8, ::1/128); X-Forwarded-For honored only from the local proxy")
		return
	}
	trustedProxies = nets
	logger.Printf("middleware: trusting X-Forwarded-For from %d proxy CIDR(s)", len(nets))
}

// peerIsTrusted reports whether ip is one of the configured trusted proxies.
func peerIsTrusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	trustedProxiesMu.RLock()
	defer trustedProxiesMu.RUnlock()
	for _, n := range trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// remoteAddrIP parses the host portion of r.RemoteAddr into a net.IP.
func remoteAddrIP(r *http.Request) net.IP {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip
		}
	}
	return net.ParseIP(r.RemoteAddr)
}

// clientIP returns the best-effort source IP for the request. The
// left-most X-Forwarded-For entry is used ONLY when the immediate TCP
// peer is a trusted proxy (see SetTrustedProxies); otherwise XFF is
// ignored and the real RemoteAddr is returned. This is what stops a
// caller that reaches the backend directly (bypassing Caddy) from
// forging X-Forwarded-For to spoof the admin IP allowlist or the
// rate-limit key. Returns nil only when neither source parses.
func clientIP(r *http.Request) net.IP {
	peer := remoteAddrIP(r)
	if peerIsTrusted(peer) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := xff
			if idx := strings.IndexByte(xff, ','); idx > 0 {
				first = xff[:idx]
			}
			if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
				return ip
			}
		}
	}
	return peer
}
