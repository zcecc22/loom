package loom

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// csrfContextKey is unexported so no other package can collide with or
// forge it.
type csrfContextKey struct{}

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfFormField  = "csrf_token"
)

// CSRFConfig configures App.CSRF.
type CSRFConfig struct {
	// Secret signs the double-submit cookie's token via HMAC-SHA256.
	// Required — App.CSRF panics if empty. There is no auto-generated
	// fallback: a randomly generated secret would silently invalidate
	// every outstanding token on process restart and break any
	// multi-replica deployment, which is worse than failing fast.
	Secret []byte

	// Secure sets the CSRF cookie's Secure attribute. Gate it
	// per-deployment (e.g. on Env == "prod"), the same convention
	// App.HSTS already uses — CSRF does not derive this from anything on
	// App itself.
	Secure bool
}

// CSRF returns a Middleware implementing CSRF protection via the
// stateless double-submit cookie pattern: no server-side session store is
// needed. On a safe request (GET, HEAD, OPTIONS), CSRF issues a
// HMAC-signed token cookie if one isn't already present and valid. On an
// unsafe request (POST, PUT, PATCH, DELETE, ...), CSRF requires the same
// token to be echoed back via the X-CSRF-Token header (or, for form
// submissions, a csrf_token form field) and rejects the request with 403
// if it's missing or doesn't match.
//
// The current token is available to a handler via Ctx.CSRFToken, for
// embedding into a hidden form field or a meta tag.
//
// The CSRF cookie is issued with SameSite=Lax, which already blocks the
// cross-site form submission this middleware defends against; CSRF
// protection here is defense-in-depth on top of that, and — like
// SameSite=Lax itself — targets same-site form/AJAX flows. A credentialed
// cross-origin API (App.CORS's AllowCredentials) needs different cookie
// attributes than this middleware sets and is out of scope here: Lax
// means the cookie is not sent on cross-site subrequests at all, so a
// credentialed cross-origin write will fail at the cookie-never-arrives
// stage, not at the token-mismatch stage.
//
// App.CSRF panics immediately, before returning the Middleware, if
// cfg.Secret is empty — a construction-time misconfiguration, not a
// per-request condition.
func (a *App) CSRF(cfg CSRFConfig) Middleware {
	if len(cfg.Secret) == 0 {
		panic("loom: CSRFConfig.Secret must not be empty")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, cookieErr := r.Cookie(csrfCookieName)
			cookieValid := cookieErr == nil && validCSRFToken(cookie.Value, cfg.Secret)

			if isSafeCSRFMethod(r.Method) {
				token := ""
				if cookieValid {
					token = cookie.Value
				} else {
					// Only issue/refresh when absent or invalid — rotating
					// on every response would invalidate a token already
					// embedded in a rendered page while an earlier request
					// referencing it is still in flight.
					token = newCSRFToken(cfg.Secret)
					http.SetCookie(w, &http.Cookie{
						Name:     csrfCookieName,
						Value:    token,
						Path:     "/",
						HttpOnly: true,
						Secure:   cfg.Secure,
						SameSite: http.SameSiteLaxMode,
					})
				}
				ctx := context.WithValue(r.Context(), csrfContextKey{}, token)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if !cookieValid {
				writeCSRFRejection(a, w, r)
				return
			}

			submitted := r.Header.Get(csrfHeaderName)
			if submitted == "" && isFormContentType(r) {
				submitted = r.FormValue(csrfFormField)
			}

			if submitted == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(submitted)) != 1 {
				writeCSRFRejection(a, w, r)
				return
			}

			ctx := context.WithValue(r.Context(), csrfContextKey{}, cookie.Value)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFToken returns the current valid CSRF token for r, and whether the
// CSRF middleware ran for this request. It is the package-level
// counterpart to Ctx.CSRFToken, for code holding only a request — such as
// middleware or a handler mounted with HandleHTTP.
func CSRFToken(r *http.Request) (token string, ok bool) {
	token, ok = r.Context().Value(csrfContextKey{}).(string)
	return token, ok
}

func isSafeCSRFMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// isFormContentType reports whether r's body is form-encoded, so the CSRF
// middleware only falls back to r.FormValue (which drains r.Body) for form
// submissions — never for a JSON API request, whose body Ctx.Bind still
// needs to read downstream.
func isFormContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") ||
		strings.HasPrefix(ct, "multipart/form-data")
}

func writeCSRFRejection(a *App, w http.ResponseWriter, r *http.Request) {
	if err := writeJSONError(w, http.StatusForbidden, statusMessage(http.StatusForbidden)); err != nil {
		a.logger.Error("loom: write error response failed", "err", err, "method", r.Method, "path", r.URL.Path)
	}
}

// newCSRFToken generates a fresh HMAC-signed token: a random 32-byte
// nonce, base64url-encoded, followed by "." and the base64url-encoded
// HMAC-SHA256 of the nonce keyed by secret.
func newCSRFToken(secret []byte) string {
	nonce := make([]byte, 32)
	// crypto/rand.Read never returns an error and always fills nonce
	// entirely, per its documented contract.
	_, _ = rand.Read(nonce)
	return encodeCSRFToken(nonce, secret)
}

func encodeCSRFToken(nonce, secret []byte) string {
	sig := signCSRFNonce(nonce, secret)
	return base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func signCSRFNonce(nonce, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(nonce)
	return mac.Sum(nil)
}

// validCSRFToken reports whether token is a genuine token signed with
// secret: well-formed, and its signature matches the nonce it carries.
// Malformed input returns false rather than panicking.
func validCSRFToken(token string, secret []byte) bool {
	nonceB64, sigB64, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	nonce, err := base64.RawURLEncoding.DecodeString(nonceB64)
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	return hmac.Equal(sig, signCSRFNonce(nonce, secret))
}
