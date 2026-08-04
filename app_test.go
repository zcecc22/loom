package loom

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// clearServerConfigEnv pins every environment variable LoadServerConfig
// reads to "unset", so a test asserting an App resolved to the hardcoded
// defaults can't be flipped by an ambient ADDR/ENV/*_TIMEOUT in the shell
// or CI runner — New now reads these on every call, including a bare
// New().
func clearServerConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ADDR", "ENV", "READ_TIMEOUT", "READ_HEADER_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT", "SHUTDOWN_TIMEOUT"} {
		t.Setenv(key, "")
	}
}

func TestNewWithNoArgument(t *testing.T) {
	clearServerConfigEnv(t)
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
	clearServerConfigEnv(t)
	a := New(AppConfig{Server: ServerConfig{Addr: ":9000"}})

	want := defaultServerConfig()
	want.Addr = ":9000"
	if a.cfg != want {
		t.Fatalf("cfg = %+v, want %+v", a.cfg, want)
	}
}

func TestAppConfigEachTimeoutFieldIndependentlyOverridable(t *testing.T) {
	clearServerConfigEnv(t)
	a := New(AppConfig{Server: ServerConfig{
		ReadTimeout:       1 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
		ShutdownTimeout:   5 * time.Second,
	}})

	base := defaultServerConfig()
	want := ServerConfig{
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
	// No clearServerConfigEnv guard needed: every field below is non-zero,
	// so resolveServerConfig never falls through to the environment.
	loaded := ServerConfig{
		Addr:              ":8080",
		Env:               "prod",
		ReadTimeout:       11 * time.Second,
		ReadHeaderTimeout: 12 * time.Second,
		WriteTimeout:      13 * time.Second,
		IdleTimeout:       14 * time.Second,
		ShutdownTimeout:   15 * time.Second,
	}

	a := New(AppConfig{Server: loaded})

	if a.cfg != loaded {
		t.Fatalf("cfg = %+v, want %+v (every field was already set, so nothing should change)", a.cfg, loaded)
	}
}

func TestNewPicksUpEnvironmentAutomatically(t *testing.T) {
	t.Setenv("ADDR", ":9191")
	a := New()
	if a.cfg.Addr != ":9191" {
		t.Fatalf("cfg.Addr = %q, want :9191 (New should read ADDR from the environment with no AppConfig at all)", a.cfg.Addr)
	}
}

func TestNewExplicitServerFieldOverridesEnvironment(t *testing.T) {
	t.Setenv("ADDR", ":9191")
	a := New(AppConfig{Server: ServerConfig{Addr: ":7000"}})
	if a.cfg.Addr != ":7000" {
		t.Fatalf("cfg.Addr = %q, want :7000 (an explicit AppConfig.Server field should win over the environment)", a.cfg.Addr)
	}
}

func TestNewPanicsOnMalformedEnvTimeout(t *testing.T) {
	t.Setenv("WRITE_TIMEOUT", "not-a-duration")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected New() to panic for a malformed WRITE_TIMEOUT")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "WRITE_TIMEOUT") {
			t.Fatalf("panic message = %q, want it to name WRITE_TIMEOUT", msg)
		}
	}()
	New()
}

func TestNewAmbientEnvProdSelectsJSONLogger(t *testing.T) {
	t.Setenv("ENV", "prod")
	a := New()
	if got := fmt.Sprintf("%T", a.Logger().Handler()); got != "*slog.JSONHandler" {
		t.Fatalf("handler type = %s, want *slog.JSONHandler (New should pick up ENV=prod from the environment with no AppConfig.Logger set)", got)
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
