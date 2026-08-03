package gwt

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
