package loom

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func csrfCookieFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookieName {
			return c
		}
	}
	return nil
}

func TestCSRFSafeMethodIssuesTokenCookie(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	a.GET("/page", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret"), Secure: true}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))

	cookie := csrfCookieFrom(rec)
	if cookie == nil {
		t.Fatal("expected a csrf_token cookie to be set")
	}
	if cookie.Value == "" {
		t.Fatal("expected a non-empty csrf_token cookie value")
	}
	if !cookie.HttpOnly {
		t.Fatal("expected the csrf_token cookie to be HttpOnly")
	}
	if !cookie.Secure {
		t.Fatal("expected the csrf_token cookie to be Secure, since CSRFConfig.Secure was true")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want SameSiteLaxMode", cookie.SameSite)
	}
	if !validCSRFToken(cookie.Value, []byte("test-secret")) {
		t.Fatal("issued token does not validate against the configured secret")
	}
}

func TestCSRFSafeMethodDoesNotRotateValidToken(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	a.GET("/page", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	first := httptest.NewRecorder()
	a.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/page", nil))
	token := csrfCookieFrom(first)
	if token == nil {
		t.Fatal("expected a csrf_token cookie on the first request")
	}

	second := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	req.AddCookie(token)
	a.ServeHTTP(second, req)

	if got := csrfCookieFrom(second); got != nil {
		t.Fatalf("expected no new csrf_token cookie for an already-valid token, got %q", got.Value)
	}
}

func TestCSRFUnsafeMethodAcceptsValidTokenViaHeader(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var handlerRan bool
	a.GET("/page", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))
	a.POST("/page", func(c *Ctx) error {
		handlerRan = true
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	getRec := httptest.NewRecorder()
	a.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/page", nil))
	cookie := csrfCookieFrom(getRec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/page", nil)
	req.AddCookie(cookie)
	req.Header.Set(csrfHeaderName, cookie.Value)
	a.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !handlerRan {
		t.Fatal("expected the handler to run for a valid header token")
	}
}

func TestCSRFUnsafeMethodAcceptsValidTokenViaFormField(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var handlerRan bool
	a.GET("/page", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))
	a.POST("/page", func(c *Ctx) error {
		handlerRan = true
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	getRec := httptest.NewRecorder()
	a.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/page", nil))
	cookie := csrfCookieFrom(getRec)

	form := url.Values{csrfFormField: {cookie.Value}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/page", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	a.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !handlerRan {
		t.Fatal("expected the handler to run for a valid form-field token")
	}
}

func TestCSRFUnsafeMethodDoesNotConsumeJSONBodyLookingForFormField(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var gotBody string
	a.GET("/page", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))
	a.POST("/page", func(c *Ctx) error {
		var in struct {
			Message string `json:"message"`
		}
		if err := c.Bind(&in); err != nil {
			return err
		}
		gotBody = in.Message
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	getRec := httptest.NewRecorder()
	a.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/page", nil))
	cookie := csrfCookieFrom(getRec)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/page", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, cookie.Value)
	req.AddCookie(cookie)
	a.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if gotBody != "hello" {
		t.Fatalf("gotBody = %q, want %q — the JSON body must survive untouched", gotBody, "hello")
	}
}

func TestCSRFUnsafeMethodMissingTokenRejected(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var handlerRan bool
	a.POST("/page", func(c *Ctx) error {
		handlerRan = true
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/page", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if handlerRan {
		t.Fatal("handler ran despite a missing CSRF token")
	}
}

func TestCSRFUnsafeMethodInvalidTokenRejected(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var handlerRan bool
	a.POST("/page", func(c *Ctx) error {
		handlerRan = true
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/page", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tampered.value"})
	req.Header.Set(csrfHeaderName, "tampered.value")
	a.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if handlerRan {
		t.Fatal("handler ran despite a tampered CSRF cookie")
	}
}

func TestCSRFUnsafeMethodMismatchedHeaderAndCookieRejected(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var handlerRan bool
	a.GET("/page", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))
	a.POST("/page", func(c *Ctx) error {
		handlerRan = true
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	getRec := httptest.NewRecorder()
	a.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/page", nil))
	cookie := csrfCookieFrom(getRec)

	other := httptest.NewRecorder()
	a.ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/page", nil))
	otherCookie := csrfCookieFrom(other)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/page", nil)
	req.AddCookie(cookie)
	req.Header.Set(csrfHeaderName, otherCookie.Value)
	a.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if handlerRan {
		t.Fatal("handler ran despite a header token that doesn't match the cookie")
	}
}

func TestCSRFPanicsOnEmptySecret(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	defer func() {
		if recover() == nil {
			t.Fatal("expected App.CSRF to panic on an empty Secret")
		}
	}()
	a.CSRF(CSRFConfig{})
}

func TestValidCSRFTokenAcceptsGenuineTokenRejectsTamperedNonceAndTamperedSignatureAndWrongSecret(t *testing.T) {
	secret := []byte("test-secret")
	token := newCSRFToken(secret)

	if !validCSRFToken(token, secret) {
		t.Fatal("a genuine token must validate")
	}

	nonceB64, sigB64, ok := strings.Cut(token, ".")
	if !ok {
		t.Fatalf("malformed token from newCSRFToken: %q", token)
	}

	tamperedNonce := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA." + sigB64
	if validCSRFToken(tamperedNonce, secret) {
		t.Fatal("a tampered nonce must not validate")
	}

	tamperedSig := nonceB64 + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if validCSRFToken(tamperedSig, secret) {
		t.Fatal("a tampered signature must not validate")
	}

	if validCSRFToken(token, []byte("wrong-secret")) {
		t.Fatal("a token signed with a different secret must not validate")
	}

	for _, malformed := range []string{"", "no-dot-here", "a.b.c", "not-base64!.also-not-base64!"} {
		if validCSRFToken(malformed, secret) {
			t.Fatalf("malformed token %q must not validate", malformed)
		}
	}
}

func TestCSRFTokenWithoutMiddlewareReturnsNotOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	token, ok := CSRFToken(req)
	if ok {
		t.Fatalf("CSRFToken() = (%q, true), want ok=false for a request that never went through CSRF", token)
	}
}

func TestCtxCSRFTokenDelegatesToPackageLevelFunction(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var gotToken string
	var gotOK bool
	a.GET("/page", func(c *Ctx) error {
		gotToken, gotOK = c.CSRFToken()
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))

	if !gotOK || gotToken == "" {
		t.Fatalf("Ctx.CSRFToken() = (%q, %v), want a non-empty token and ok=true", gotToken, gotOK)
	}
	cookie := csrfCookieFrom(rec)
	if cookie == nil || cookie.Value != gotToken {
		t.Fatalf("Ctx.CSRFToken() = %q, want it to match the issued cookie value", gotToken)
	}
}
