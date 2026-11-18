package loom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestSessions(t *testing.T, secure bool) (*Sessions, *fakeClock) {
	t.Helper()
	s := NewSessions(SessionConfig{IdleTimeout: 15 * time.Minute, MaxLifetime: time.Hour, Secure: secure})
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s.now = clk.now
	return s, clk
}

// startSession runs Create and returns a request carrying the cookie it set.
func startSession(t *testing.T, s *Sessions, identity string) (*http.Request, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := s.Create(rec, identity); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Create set %d cookies", len(cookies))
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookies[0])
	return req, cookies[0]
}

func TestSessionsCreateLookupDestroy(t *testing.T) {
	s, _ := newTestSessions(t, true)
	req, c := startSession(t, s, "owner")

	if c.Name != "__Host-session" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 3600 {
		t.Fatalf("cookie attributes %+v", c)
	}
	if len(c.Value) < 43 {
		t.Fatalf("token too short: %q", c.Value)
	}
	if id, ok := s.Lookup(req); !ok || id != "owner" {
		t.Fatalf("Lookup = %q, %v", id, ok)
	}

	rec := httptest.NewRecorder()
	s.Destroy(rec, req)
	if _, ok := s.Lookup(req); ok {
		t.Fatal("session survived Destroy")
	}
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge != -1 {
		t.Fatalf("Destroy did not clear the cookie: %+v", cleared)
	}
}

func TestSessionsInsecureCookieName(t *testing.T) {
	s, _ := newTestSessions(t, false)
	_, c := startSession(t, s, "owner")
	if c.Name != "session" || c.Secure {
		t.Fatalf("cookie %+v", c)
	}
}

func TestSessionsIdleTimeout(t *testing.T) {
	s, clk := newTestSessions(t, true)
	req, _ := startSession(t, s, "owner")

	clk.advance(14 * time.Minute)
	if _, ok := s.Lookup(req); !ok {
		t.Fatal("expired before idle timeout")
	}
	clk.advance(14 * time.Minute) // 28 min since start, 14 since last use
	if _, ok := s.Lookup(req); !ok {
		t.Fatal("use did not restart the idle timer")
	}
	clk.advance(15 * time.Minute)
	if _, ok := s.Lookup(req); ok {
		t.Fatal("alive after idle timeout")
	}
}

func TestSessionsMaxLifetime(t *testing.T) {
	s, clk := newTestSessions(t, true)
	req, _ := startSession(t, s, "owner")
	for i := 0; i < 5; i++ {
		clk.advance(10 * time.Minute)
		if _, ok := s.Lookup(req); !ok {
			t.Fatalf("expired early at step %d", i)
		}
	}
	clk.advance(10 * time.Minute) // 60 min
	if _, ok := s.Lookup(req); ok {
		t.Fatal("alive past MaxLifetime despite activity")
	}
}

func TestSessionsRejectUnknownAndForgedTokens(t *testing.T) {
	s, _ := newTestSessions(t, true)
	startSession(t, s, "owner")
	for _, v := range []string{"", "x", strings.Repeat("A", 43)} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "__Host-session", Value: v})
		if _, ok := s.Lookup(req); ok {
			t.Fatalf("token %q accepted", v)
		}
	}
	if _, ok := s.Lookup(httptest.NewRequest(http.MethodGet, "/", nil)); ok {
		t.Fatal("request without cookie accepted")
	}
}

func TestSessionsDestroyIdentityAndSweep(t *testing.T) {
	s, clk := newTestSessions(t, true)
	r1, _ := startSession(t, s, "a")
	r2, _ := startSession(t, s, "a")
	r3, _ := startSession(t, s, "b")
	if n := s.DestroyIdentity("a"); n != 2 {
		t.Fatalf("DestroyIdentity ended %d", n)
	}
	if _, ok := s.Lookup(r1); ok {
		t.Fatal("r1 survived")
	}
	if _, ok := s.Lookup(r2); ok {
		t.Fatal("r2 survived")
	}
	if _, ok := s.Lookup(r3); !ok {
		t.Fatal("other identity's session ended")
	}
	clk.advance(2 * time.Hour)
	if n := s.Len(); n != 0 {
		t.Fatalf("Len after expiry = %d", n)
	}
}

func TestSessionsTokensAreUnique(t *testing.T) {
	s, _ := newTestSessions(t, true)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		_, c := startSession(t, s, "owner")
		if seen[c.Value] {
			t.Fatal("duplicate token")
		}
		seen[c.Value] = true
	}
}

func TestSessionsLookupWorksAsAuthentication(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	s, _ := newTestSessions(t, true)
	a.GET("/private", func(c *Ctx) error {
		id, _ := c.Identity()
		return c.String(200, id)
	}, a.Authentication(s.Lookup))
	req, _ := startSession(t, s, "owner")
	req.URL.Path = "/private"
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "owner" {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/private", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-cookie status %d", rec.Code)
	}
}

func TestNewSessionsPanicsOnMisconfiguration(t *testing.T) {
	for name, cfg := range map[string]SessionConfig{
		"zero":             {},
		"no idle":          {MaxLifetime: time.Hour},
		"no max":           {IdleTimeout: time.Minute},
		"max shorter idle": {IdleTimeout: time.Hour, MaxLifetime: time.Minute},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			NewSessions(cfg)
		}()
	}
}

func TestCSPSetsHeaderAndPanicsWhenEmpty(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	const policy = "default-src 'none'; script-src 'self'"
	a.Use(a.CSP(policy))
	a.GET("/x", func(c *Ctx) error { return c.String(200, "ok") })
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != policy {
		t.Fatalf("CSP %q", got)
	}
	rec = httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if got := rec.Header().Get("Content-Security-Policy"); got != policy {
		t.Fatalf("CSP missing on mux 404: %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("empty policy did not panic")
		}
	}()
	a.CSP("  ")
}
