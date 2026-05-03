package middleware

import (
	"net"
	"net/http"
	"strings"

	"mootd/backend/internal/shared/response"
)

// AdminIPAllowlist returns middleware that 403s any request
// whose source IP isn't in the configured CIDR allowlist
// (P5-03 / mootd-admin#36). Defense-in-depth: the recommended
// production deployment also enforces the same list at the
// Caddy reverse-proxy layer (see docs/RUNBOOKS/admin-deployment.md),
// so an attacker would need to bypass Caddy AND have a valid
// admin JWT to reach the protected handlers — but having the
// gate inside the binary too means single-host deploys still
// get the protection without manual proxy config.
//
// `cidrs` is a list of CIDR strings (e.g. "203.0.113.0/24",
// "192.168.1.42/32", "::1/128"). An empty / nil list disables
// the allowlist (allow-all). Invalid CIDRs are silently dropped
// at parse time + logged once, rather than failing startup —
// a typo in env shouldn't keep the admin out entirely.
//
// Source IP precedence: the left-most X-Forwarded-For entry
// when present (Caddy / Cloudflare populate it), else
// RemoteAddr. Same convention as admin.clientIP() — kept here
// without the import so this middleware doesn't drag the admin
// package into shared/.
//
// Failures return 403 with a deliberately uninformative
// message ("forbidden") so an attacker can't probe whether
// they're hitting an allowlist vs. a different gate.
func AdminIPAllowlist(cidrs []string, logger interface{ Printf(string, ...any) }) func(http.Handler) http.Handler {
	if logger == nil {
		logger = noopLogger{}
	}
	nets := parseCIDRs(cidrs, logger)
	if len(nets) == 0 {
		// Allow-all path. Return identity middleware so callers
		// can wire this unconditionally without a nil check.
		logger.Printf("middleware: admin IP allowlist disabled (no CIDRs configured)")
		return func(next http.Handler) http.Handler { return next }
	}
	logger.Printf("middleware: admin IP allowlist enforcing %d CIDR(s)", len(nets))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := remoteIP(r)
			if ip == nil {
				logger.Printf("middleware: admin IP allowlist: no parseable client IP, denying")
				response.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
			for _, n := range nets {
				if n.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
			logger.Printf("middleware: admin IP allowlist: %s not in allowlist (path=%s)", ip, r.URL.Path)
			response.WriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		})
	}
}

// parseCIDRs converts the env-string list to *net.IPNet. Single
// IPs without a /N suffix are upgraded to /32 (v4) or /128 (v6).
func parseCIDRs(cidrs []string, logger interface{ Printf(string, ...any) }) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			// Treat bare IP as /32 or /128.
			ip := net.ParseIP(s)
			if ip == nil {
				logger.Printf("middleware: admin IP allowlist: dropping unparseable entry %q", raw)
				continue
			}
			if ip.To4() != nil {
				s = ip.String() + "/32"
			} else {
				s = ip.String() + "/128"
			}
		}
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			logger.Printf("middleware: admin IP allowlist: dropping unparseable CIDR %q: %v", raw, err)
			continue
		}
		out = append(out, ipnet)
	}
	return out
}

// remoteIP extracts the client IP. Same precedence rules as
// admin.clientIP() — left-most X-Forwarded-For entry when set
// (trusted because Caddy / Cloudflare append; not because the
// header itself is trustworthy), else RemoteAddr.
func remoteIP(r *http.Request) net.IP {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.IndexByte(xff, ','); idx > 0 {
			xff = xff[:idx]
		}
		if ip := net.ParseIP(strings.TrimSpace(xff)); ip != nil {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip
		}
	}
	if ip := net.ParseIP(r.RemoteAddr); ip != nil {
		return ip
	}
	return nil
}

// noopLogger is a fallback when callers don't pass a logger.
type noopLogger struct{}

func (noopLogger) Printf(string, ...any) {}
