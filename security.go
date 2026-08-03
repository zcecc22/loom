package gwt

import "net/http"

// SecureHeaders returns a Middleware that sets a small set of
// browser-facing defensive headers: X-Content-Type-Options (blocks
// MIME-sniffing), X-Frame-Options (blocks clickjacking via iframes), and
// Referrer-Policy (limits what's leaked in the Referer header on
// cross-origin navigation). These protect the browser, independent of
// server/proxy topology — they're just as relevant behind a reverse proxy
// as without one.
//
// Deliberately not included: X-XSS-Protection (deprecated — the legacy
// browser filter it enabled introduced vulnerabilities of its own, so
// modern guidance is to omit it, not set it to any value) and
// Content-Security-Policy / Permissions-Policy (no safe one-size-fits-all
// default exists; a wrong CSP breaks pages more often than it helps, so
// it's left to the caller to add if and how their app needs it).
//
// SecureHeaders is opt-in via Use, not automatic like Logging/Recover —
// unlike those, a header such as X-Frame-Options can break a legitimate
// use case (e.g. intentional iframe embedding), so it isn't safe to force
// on every App by default.
func (a *App) SecureHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBodySize returns a Middleware that limits request bodies to n bytes
// via http.MaxBytesReader, so a single request can't exhaust server
// memory. This is defense-in-depth: a reverse proxy in front of the App
// may or may not already enforce a body-size limit, and this doesn't rely
// on that being configured correctly.
func (a *App) MaxBodySize(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// HSTS returns a Middleware that sets Strict-Transport-Security, telling
// browsers to only ever connect to this host over HTTPS. It is only
// correct to use when this deployment is actually reached over HTTPS —
// directly or via a TLS-terminating reverse proxy — which is why it's a
// separate opt-in from SecureHeaders rather than bundled with it: enabling
// it for a plain-HTTP deployment (e.g. local development) would be wrong,
// not just unnecessary.
func (a *App) HSTS() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			next.ServeHTTP(w, r)
		})
	}
}
