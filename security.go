package loom

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

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

// defaultCORSMethods is used for Access-Control-Allow-Methods when
// CORSConfig.AllowedMethods is empty — the six loom verb methods.
var defaultCORSMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodOptions,
}

// CORSConfig configures App.CORS. AllowedOrigins is an exact-match list —
// no pattern matching beyond the literal "*" wildcard, which is only valid
// when AllowCredentials is false: CORS itself forbids combining a
// credentialed response with a wildcard origin, and App.CORS panics at
// setup if the two are combined rather than producing a browser-rejected
// response at request time.
type CORSConfig struct {
	// AllowedOrigins lists the exact origins allowed to make cross-origin
	// requests, e.g. "https://example.com". "*" allows any origin, but
	// only when AllowCredentials is false.
	AllowedOrigins []string

	// AllowedMethods sets Access-Control-Allow-Methods on a preflight
	// response. Defaults to loom's six verb methods (GET, POST, PUT,
	// PATCH, DELETE, OPTIONS) if empty.
	AllowedMethods []string

	// AllowedHeaders sets Access-Control-Allow-Headers on a preflight
	// response. If empty, the preflight's own Access-Control-Request-Headers
	// value is reflected back verbatim — a standard, low-risk default.
	// Note: if you set this explicitly and also use App.CSRF, include
	// "X-CSRF-Token" here, or every cross-origin CSRF-protected write will
	// fail as a browser-side CORS rejection rather than a CSRF 403.
	AllowedHeaders []string

	// ExposedHeaders sets Access-Control-Expose-Headers on actual (non-
	// preflight) responses, letting client script read headers beyond the
	// small CORS-safelisted set.
	ExposedHeaders []string

	// AllowCredentials sets Access-Control-Allow-Credentials: true, and
	// forbids "*" in AllowedOrigins (see AllowedOrigins).
	AllowCredentials bool

	// MaxAge sets Access-Control-Max-Age on a preflight response, telling
	// the browser how long it may cache the preflight result. Rendered as
	// whole seconds; zero omits the header.
	MaxAge time.Duration
}

// CORS returns a Middleware implementing Cross-Origin Resource Sharing.
// CORS is a browser-enforced restriction, not a server-side gate: a
// request from a disallowed origin is still served normally (the browser
// withholds the response from the requesting script), and a preflight
// request is always answered, allowed or not — CORS never rejects a
// request with an error status itself.
//
// CORS must be wired via Use (so it wraps the whole built handler) rather
// than as route-level middleware, since a preflight OPTIONS request never
// reaches route dispatch — see the note on preflight handling below.
//
// App.CORS panics immediately, before returning the Middleware, if
// cfg.AllowCredentials is true and cfg.AllowedOrigins contains "*" — a
// combination CORS itself forbids. This is a construction-time
// misconfiguration, not a per-request condition, so it fails at setup
// rather than being silently unenforceable at runtime.
func (a *App) CORS(cfg CORSConfig) Middleware {
	wildcard := false
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			wildcard = true
			break
		}
	}
	if wildcard && cfg.AllowCredentials {
		panic("loom: CORSConfig.AllowCredentials cannot be combined with a wildcard (\"*\") AllowedOrigins entry")
	}

	methods := cfg.AllowedMethods
	if len(methods) == 0 {
		methods = defaultCORSMethods
	}
	allowMethods := strings.Join(methods, ", ")
	allowHeaders := strings.Join(cfg.AllowedHeaders, ", ")
	exposeHeaders := strings.Join(cfg.ExposedHeaders, ", ")
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.Itoa(int(cfg.MaxAge.Seconds()))
	}

	allowedOrigin := func(origin string) (value string, ok bool) {
		if origin == "" {
			return "", false
		}
		if wildcard && !cfg.AllowCredentials {
			return "*", true
		}
		for _, o := range cfg.AllowedOrigins {
			if o == origin {
				return origin, true
			}
		}
		return "", false
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			preflight := r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != ""

			value, ok := allowedOrigin(origin)

			if preflight {
				h := w.Header()
				h.Add("Vary", "Origin")
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				if ok {
					h.Set("Access-Control-Allow-Origin", value)
					if cfg.AllowCredentials {
						h.Set("Access-Control-Allow-Credentials", "true")
					}
					h.Set("Access-Control-Allow-Methods", allowMethods)
					if allowHeaders != "" {
						h.Set("Access-Control-Allow-Headers", allowHeaders)
					} else if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
						h.Set("Access-Control-Allow-Headers", reqHeaders)
					}
					if maxAge != "" {
						h.Set("Access-Control-Max-Age", maxAge)
					}
				}
				// A preflight is always answered here, allowed or not — it
				// never reaches next, since there is no route to preflight
				// against and the mux would otherwise answer with its own
				// 404/405.
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if !ok {
				// Not a preflight and the origin isn't allowed: CORS is
				// enforced by the browser, not the server, so the request
				// still runs normally. The absence of the Allow-Origin
				// header is itself origin-dependent, so Vary still applies.
				w.Header().Add("Vary", "Origin")
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", value)
			if cfg.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if exposeHeaders != "" {
				h.Set("Access-Control-Expose-Headers", exposeHeaders)
			}
			if value != "*" {
				// A "*" response is identical regardless of origin, so no
				// Vary is needed; an echoed single-origin response is
				// origin-dependent and must be, or a shared cache could
				// serve one origin's response to another.
				h.Add("Vary", "Origin")
			}
			next.ServeHTTP(w, r)
		})
	}
}
