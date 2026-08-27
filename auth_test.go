package loom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthenticationRejectsUnauthenticatedRequest(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var handlerCalled bool
	a.GET("/admin", func(c *Ctx) error {
		handlerCalled = true
		return nil
	}, a.Authentication(func(r *http.Request) (string, bool) {
		return "", false
	}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if handlerCalled {
		t.Fatal("handler ran despite failed authentication")
	}
}

func TestAuthenticationAcceptsAndSetsIdentity(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var gotIdentity string
	var gotOK bool
	a.GET("/admin", func(c *Ctx) error {
		gotIdentity, gotOK = Identity(c.R)
		return c.NoContent(http.StatusOK)
	}, a.Authentication(func(r *http.Request) (string, bool) {
		return "alice", true
	}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !gotOK || gotIdentity != "alice" {
		t.Fatalf("Identity() = (%q, %v), want (\"alice\", true)", gotIdentity, gotOK)
	}
}

func TestIdentityWithoutAuthenticationReturnsNotOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	identity, ok := Identity(req)
	if ok {
		t.Fatalf("Identity() = (%q, true), want ok=false for a request that never went through Authentication", identity)
	}
}

func TestAuthenticationWithRealBasicAuthPattern(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	a.GET("/admin", func(c *Ctx) error {
		user, _ := c.Identity()
		return c.String(http.StatusOK, "hello "+user)
	}, a.Authentication(func(r *http.Request) (string, bool) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			return "", false
		}
		return user, true
	}))

	// No credentials.
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credentials: status = %d, want 401", rec.Code)
	}

	// Wrong credentials.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.SetBasicAuth("admin", "wrong")
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credentials: status = %d, want 401", rec.Code)
	}

	// Correct credentials.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.SetBasicAuth("admin", "secret")
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct credentials: status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "hello admin" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "hello admin")
	}
}

func TestAuthenticationRedirectSendsSeeOtherOnFailure(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var handlerCalled bool
	a.GET("/admin", func(c *Ctx) error {
		handlerCalled = true
		return nil
	}, a.AuthenticationRedirect(AuthenticationRedirectConfig{
		Authenticate: func(r *http.Request) (string, bool) { return "", false },
		LoginPath:    "/login",
	}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want %q", got, "/login")
	}
	if got := rec.Body.String(); strings.HasPrefix(got, "{") {
		t.Fatalf("body = %q, want stdlib's plain redirect body, not loom's JSON error shape", got)
	}
	if handlerCalled {
		t.Fatal("handler ran despite failed authentication")
	}
}

func TestAuthenticationRedirectAcceptsAndSetsIdentity(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var gotIdentity string
	var gotOK bool
	a.GET("/admin", func(c *Ctx) error {
		gotIdentity, gotOK = Identity(c.R)
		return c.NoContent(http.StatusOK)
	}, a.AuthenticationRedirect(AuthenticationRedirectConfig{
		Authenticate: func(r *http.Request) (string, bool) { return "alice", true },
		LoginPath:    "/login",
	}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !gotOK || gotIdentity != "alice" {
		t.Fatalf("Identity() = (%q, %v), want (\"alice\", true)", gotIdentity, gotOK)
	}
}

func TestAuthenticationRedirectPanicsOnNilAuthenticateOrUnsafeLoginPath(t *testing.T) {
	cases := []struct {
		name string
		cfg  AuthenticationRedirectConfig
	}{
		{"nil Authenticate", AuthenticationRedirectConfig{LoginPath: "/login"}},
		{"empty LoginPath", AuthenticationRedirectConfig{Authenticate: func(r *http.Request) (string, bool) { return "", true }}},
		{"relative LoginPath", AuthenticationRedirectConfig{
			Authenticate: func(r *http.Request) (string, bool) { return "", true },
			LoginPath:    "login",
		}},
		{"protocol-relative LoginPath (open redirect)", AuthenticationRedirectConfig{
			Authenticate: func(r *http.Request) (string, bool) { return "", true },
			LoginPath:    "//evil.example.com",
		}},
	}

	a := New(AppConfig{Logger: discardLogger()})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected AuthenticationRedirect to panic")
				}
			}()
			a.AuthenticationRedirect(tc.cfg)
		})
	}
}
