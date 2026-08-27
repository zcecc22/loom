package loom

import (
	"context"
	"net/http"
	"strings"
)

// identityContextKey is unexported so no other package can collide with
// or forge it.
type identityContextKey struct{}

// Authentication returns a Middleware that calls authenticate for every
// request it wraps. If authenticate reports !ok, Authentication responds
// 401 and next is never called. If it reports ok, the returned identity
// is stashed in the request's context (read it back with Identity) and
// next runs.
//
// authenticate is where the actual credential check lives — Basic auth via
// r.BasicAuth(), a bearer token from the Authorization header, a signed
// cookie, whatever the caller needs. Authentication itself is
// scheme-agnostic: a rejection is a 401 carrying loom's usual JSON error
// body and no WWW-Authenticate header, since Authentication has no way to
// know which scheme authenticate implements.
//
// Authentication is route-level middleware, passed as a trailing argument
// to GET/POST/etc. — it is not global or automatic. Protecting only some
// routes is just a matter of only passing it to those routes' calls; no
// separate grouping mechanism is needed.
func (a *App) Authentication(authenticate func(r *http.Request) (identity string, ok bool)) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := authenticate(r)
			if !ok {
				// Rendered as JSON, matching every other loom-generated
				// error, so a client doesn't have to special-case the
				// rejection shape.
				if werr := writeJSONError(w, http.StatusUnauthorized, statusMessage(http.StatusUnauthorized)); werr != nil {
					a.logger.Error("loom: write error response failed", "err", werr, "method", r.Method, "path", r.URL.Path)
				}
				return
			}
			ctx := context.WithValue(r.Context(), identityContextKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthenticationRedirectConfig configures App.AuthenticationRedirect.
type AuthenticationRedirectConfig struct {
	// Authenticate performs the actual credential check — a signed cookie,
	// a session lookup, whatever the caller needs. Required;
	// AuthenticationRedirect panics if nil.
	Authenticate func(r *http.Request) (identity string, ok bool)

	// LoginPath is where a failed check redirects to, via 303 See Other.
	// Required: must be a local path starting with a single "/" (not
	// "//") — AuthenticationRedirect panics otherwise, since a
	// caller-controlled off-site value would be a fixed open redirect on
	// every route this protects.
	LoginPath string
}

// AuthenticationRedirect returns a Middleware like Authentication, except a
// failed check redirects to cfg.LoginPath (303 See Other) instead of
// responding with loom's usual JSON 401 — for a full-page-reload UI where a
// JSON body serves no purpose on a browser navigation. This is a
// deliberate, documented exception to "every loom-generated error response
// is JSON" (see the Conventions note in CLAUDE.md and doc.go), alongside
// HandleHTTP and ServeStatic. On success it behaves identically to
// Authentication: the returned identity is stashed in the request's
// context (read it back with Identity) and next runs.
//
// Like Authentication, this is route-level middleware — pass it only to
// the routes that need it. If wired globally via Use, an unmatched path
// also redirects to LoginPath rather than reaching loom's usual JSON 404,
// the same short-circuit-before-the-mux behavior CLAUDE.md documents for
// global CSRF.
//
// The credential/session mechanism itself is entirely cfg.Authenticate's
// responsibility — loom still issues, signs, and stores nothing.
func (a *App) AuthenticationRedirect(cfg AuthenticationRedirectConfig) Middleware {
	if cfg.Authenticate == nil {
		panic("loom: AuthenticationRedirect: cfg.Authenticate is nil")
	}
	if cfg.LoginPath == "" || !strings.HasPrefix(cfg.LoginPath, "/") || strings.HasPrefix(cfg.LoginPath, "//") {
		panic("loom: AuthenticationRedirect: LoginPath must be a local path starting with a single \"/\", got " + cfg.LoginPath)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, ok := cfg.Authenticate(r)
			if !ok {
				http.Redirect(w, r, cfg.LoginPath, http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), identityContextKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Identity returns the identity string an Authentication middleware
// stashed in r's context, and whether one was set. ok is false if r never
// passed through Authentication (or if the request context was replaced
// without carrying it along).
//
// Handlers normally use Ctx.Identity instead; this function is for code
// holding only an *http.Request, such as middleware or a handler mounted
// with HandleHTTP.
func Identity(r *http.Request) (identity string, ok bool) {
	identity, ok = r.Context().Value(identityContextKey{}).(string)
	return identity, ok
}
