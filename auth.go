package loom

import (
	"context"
	"net/http"
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
