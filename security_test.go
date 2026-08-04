package gwt

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSecureHeadersSetsExpectedHeaders(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.SecureHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Fatalf("header %s = %q, want %q", k, got, v)
		}
	}
}

func TestHSTSSetsExpectedHeader(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.HSTS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := "max-age=31536000; includeSubDomains"
	if got := rec.Header().Get("Strict-Transport-Security"); got != want {
		t.Fatalf("Strict-Transport-Security = %q, want %q", got, want)
	}
}

func TestMaxBodySizeRejectsOversizedBody(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var readErr error
	h := a.MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100)))
	h.ServeHTTP(rec, req)

	if readErr == nil {
		t.Fatal("expected reading an oversized body to error, got nil")
	}
}

func TestMaxBodySizePassesBodyUnderLimit(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var body []byte
	var readErr error
	h := a.MaxBodySize(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
	h.ServeHTTP(rec, req)

	if readErr != nil {
		t.Fatalf("unexpected error reading body under the limit: %v", readErr)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want %q", body, "hello")
	}
}

func corsNextHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCORSPreflightAllowedOriginSetsHeadersAndReturns204(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.CORS(CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedHeaders: []string{"X-CSRF-Token"},
		MaxAge:         10 * time.Minute,
	})(corsNextHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	want := map[string]string{
		"Access-Control-Allow-Origin":  "https://example.com",
		"Access-Control-Allow-Methods": "GET, POST, PUT, PATCH, DELETE, OPTIONS",
		"Access-Control-Allow-Headers": "X-CSRF-Token",
		"Access-Control-Max-Age":       "600",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Fatalf("header %s = %q, want %q", k, got, v)
		}
	}
}

func TestCORSPreflightDisallowedOriginOmitsHeadersButStillReturns204(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.CORS(CORSConfig{AllowedOrigins: []string{"https://example.com"}})(corsNextHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCORSActualRequestSetsHeadersForAllowedOrigin(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.CORS(CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowCredentials: true,
		ExposedHeaders:   []string{"X-Total-Count"},
	})(corsNextHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	want := map[string]string{
		"Access-Control-Allow-Origin":      "https://example.com",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Expose-Headers":    "X-Total-Count",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Fatalf("header %s = %q, want %q", k, got, v)
		}
	}
	if got := rec.Header().Values("Vary"); len(got) == 0 || got[0] != "Origin" {
		t.Fatalf("Vary = %v, want [Origin]", got)
	}
}

func TestCORSActualRequestDisallowedOriginPassesThroughWithoutHeaders(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.CORS(CORSConfig{AllowedOrigins: []string{"https://example.com"}})(corsNextHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, req)

	// CORS is browser-enforced, not server-enforced: the handler still
	// runs and returns its normal status.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (disallowed origin must not be rejected server-side)", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if got := rec.Header().Values("Vary"); len(got) == 0 || got[0] != "Origin" {
		t.Fatalf("Vary = %v, want [Origin] (absence of CORS headers is itself origin-dependent)", got)
	}
}

func TestCORSWildcardOriginAllowsAnyOriginWithoutCredentials(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.CORS(CORSConfig{AllowedOrigins: []string{"*"}})(corsNextHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anything.example")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	if got := rec.Header().Values("Vary"); len(got) != 0 {
		t.Fatalf("Vary = %v, want none (a wildcard response doesn't depend on origin)", got)
	}
}

func TestCORSPanicsOnWildcardOriginWithCredentials(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	defer func() {
		if recover() == nil {
			t.Fatal("expected App.CORS to panic on AllowCredentials + wildcard AllowedOrigins")
		}
	}()
	a.CORS(CORSConfig{AllowedOrigins: []string{"*"}, AllowCredentials: true})
}

func TestCORSNonPreflightOptionsReachesRegisteredHandler(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	a.Use(a.CORS(CORSConfig{AllowedOrigins: []string{"https://example.com"}}))
	ran := false
	a.OPTIONS("/items/{id}", func(c *Ctx) error {
		ran = true
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	// A plain OPTIONS probe (no Access-Control-Request-Method) is not a
	// CORS preflight, so it must reach the registered route.
	req := httptest.NewRequest(http.MethodOptions, "/items/1", nil)
	req.Header.Set("Origin", "https://example.com")
	a.Handler().ServeHTTP(rec, req)

	if !ran {
		t.Fatal("expected the registered OPTIONS handler to run for a non-preflight OPTIONS request")
	}
}
