package gwt

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewWithNoArgument(t *testing.T) {
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

func TestAppConfigPartialConfigFillsMissingFieldsWithDefaults(t *testing.T) {
	a := New(AppConfig{Config: Config{Addr: ":9000"}})

	want := defaultConfig()
	want.Addr = ":9000"
	if a.cfg != want {
		t.Fatalf("cfg = %+v, want %+v", a.cfg, want)
	}
}

func TestAppConfigEachTimeoutFieldIndependentlyOverridable(t *testing.T) {
	a := New(AppConfig{Config: Config{
		ReadTimeout:       1 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}})

	base := defaultConfig()
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

func TestAppConfigFullyResolvedConfigPassesThroughUnchanged(t *testing.T) {
	loaded := Config{
		Addr:              ":8080",
		Env:               "prod",
		ReadTimeout:       11 * time.Second,
		ReadHeaderTimeout: 12 * time.Second,
		WriteTimeout:      13 * time.Second,
		IdleTimeout:       14 * time.Second,
		ShutdownTimeout:   15 * time.Second,
	}

	a := New(AppConfig{Config: loaded})

	if a.cfg != loaded {
		t.Fatalf("cfg = %+v, want %+v (every field was already set, so nothing should change)", a.cfg, loaded)
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

	a := New(AppConfig{Logger: discardLogger()})
	a.Use(counting)
	a.GET("/a", func(c *Ctx) error { return nil })
	a.GET("/b", func(c *Ctx) error { return nil })

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
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	a.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom from Use-added middleware")
		})
	})
	a.GET("/", func(c *Ctx) error {
		t.Fatal("handler should never run — Use-added middleware panics first")
		return nil
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
	a := New(AppConfig{Logger: discardLogger()})
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
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	a.GET("/panics", func(c *Ctx) error {
		panic("boom")
	})
	a.GET("/ok", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
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
	a := New(AppConfig{Logger: custom})
	if a.Logger() != custom {
		t.Fatal("Logger() did not return the logger passed via AppConfig.Logger")
	}

	a2 := New()
	if a2.Logger() == nil {
		t.Fatal("Logger() returned nil for a default-constructed App")
	}
}
