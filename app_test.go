package gwt

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewWithZeroOptions(t *testing.T) {
	a := New()
	if a == nil {
		t.Fatal("New() returned nil")
	}
	if a.logger == nil {
		t.Fatal("New() left logger nil")
	}
	if a.mux == nil {
		t.Fatal("New() left mux nil")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty App status = %d, want 404 (empty mux)", rec.Code)
	}
}

func TestWithConfigThenGranularOverrideWinsOnlyThatField(t *testing.T) {
	loaded := Config{
		Addr:              ":8000",
		Env:               "dev",
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ShutdownTimeout:   10 * time.Second,
	}

	a := New(WithConfig(loaded), WithWriteTimeout(30*time.Second))

	if a.cfg.WriteTimeout != 30*time.Second {
		t.Fatalf("WriteTimeout = %v, want 30s", a.cfg.WriteTimeout)
	}
	if a.cfg.ReadTimeout != loaded.ReadTimeout {
		t.Fatalf("ReadTimeout = %v, want unchanged %v", a.cfg.ReadTimeout, loaded.ReadTimeout)
	}
	if a.cfg.ReadHeaderTimeout != loaded.ReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %v, want unchanged %v", a.cfg.ReadHeaderTimeout, loaded.ReadHeaderTimeout)
	}
	if a.cfg.IdleTimeout != loaded.IdleTimeout {
		t.Fatalf("IdleTimeout = %v, want unchanged %v", a.cfg.IdleTimeout, loaded.IdleTimeout)
	}
	if a.cfg.ShutdownTimeout != loaded.ShutdownTimeout {
		t.Fatalf("ShutdownTimeout = %v, want unchanged %v", a.cfg.ShutdownTimeout, loaded.ShutdownTimeout)
	}
	if a.cfg.Addr != loaded.Addr {
		t.Fatalf("Addr = %v, want unchanged %v", a.cfg.Addr, loaded.Addr)
	}
}

func TestGranularTimeoutOptionsEachOverrideOnlyOwnField(t *testing.T) {
	base := defaultConfig()
	a := New(
		WithConfig(base),
		WithReadTimeout(1*time.Second),
		WithReadHeaderTimeout(2*time.Second),
		WithWriteTimeout(3*time.Second),
		WithIdleTimeout(4*time.Second),
		WithShutdownTimeout(5*time.Second),
	)

	want := Config{
		Addr:              base.Addr,
		Env:               base.Env,
		ReadTimeout:       1 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}
	if a.cfg != want {
		t.Fatalf("cfg = %+v, want %+v", a.cfg, want)
	}
}

func TestUseWrapsWholeHandlerOnce(t *testing.T) {
	var calls int
	counting := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			next.ServeHTTP(w, r)
		})
	}

	a := New(WithLogger(discardLogger()))
	a.Use(counting)
	a.Get("/a", func(w http.ResponseWriter, r *http.Request) {})
	a.Get("/b", func(w http.ResponseWriter, r *http.Request) {})

	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/a", nil))
	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/b", nil))

	if calls != 2 {
		t.Fatalf("Use-added middleware called %d times, want 2 (once per request)", calls)
	}
}

func TestUseStacksAfterDefaultsNotBefore(t *testing.T) {
	// A panic inside Use-added middleware must still be caught by the
	// always-on Recover default, which proves Recover wraps outside
	// (before) anything registered via Use — exactly the "stacked after
	// the defaults" guarantee documented on Use.
	var buf bytes.Buffer
	a := New(WithLogger(jsonLogger(&buf)))
	a.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom from Use-added middleware")
		})
	})
	a.Get("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should never run — Use-added middleware panics first")
	})

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Recover should catch panics from Use-added middleware)", rec.Code)
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Fatalf("expected Recover's log entry, got: %s", buf.String())
	}
}

func TestHandlerCoversMuxNotFound(t *testing.T) {
	var sawRequest bool
	a := New(WithLogger(discardLogger()))
	a.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sawRequest = true
			next.ServeHTTP(w, r)
		})
	})

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !sawRequest {
		t.Fatal("Use-added middleware did not see the mux's own 404 response")
	}
}

func TestNewAutoWiresLoggingAndRecoverWithNoSetup(t *testing.T) {
	var buf bytes.Buffer
	a := New(WithLogger(jsonLogger(&buf)))
	a.Get("/panics", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	a.Get("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panics", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panicking handler status = %d, want 500 (Recover should be wired by New with zero setup)", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	a.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec2.Code)
	}

	logs := buf.String()
	if !strings.Contains(logs, "panic recovered") {
		t.Fatalf("expected Recover's log entry with zero setup, got: %s", logs)
	}
	if !strings.Contains(logs, `"msg":"request"`) {
		t.Fatalf("expected Logging's log entry with zero setup, got: %s", logs)
	}
}

func TestLoggerReturnsResolvedLogger(t *testing.T) {
	custom := discardLogger()
	a := New(WithLogger(custom))
	if a.Logger() != custom {
		t.Fatal("Logger() did not return the logger passed via WithLogger")
	}

	a2 := New()
	if a2.Logger() == nil {
		t.Fatal("Logger() returned nil for a default-constructed App")
	}
}
