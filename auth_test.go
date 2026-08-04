package gwt

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthRejectsUnauthenticatedRequest(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var handlerCalled bool
	a.GET("/admin", func(c *Ctx) error {
		handlerCalled = true
		return nil
	}, a.Auth(func(r *http.Request) (string, bool) {
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

func TestAuthAcceptsAndSetsIdentity(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var gotIdentity string
	var gotOK bool
	a.GET("/admin", func(c *Ctx) error {
		gotIdentity, gotOK = Identity(c.R)
		return c.NoContent(http.StatusOK)
	}, a.Auth(func(r *http.Request) (string, bool) {
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

func TestIdentityWithoutAuthReturnsNotOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	identity, ok := Identity(req)
	if ok {
		t.Fatalf("Identity() = (%q, true), want ok=false for a request that never went through Auth", identity)
	}
}

func TestAuthWithRealBasicAuthPattern(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.GET("/admin", func(c *Ctx) error {
		user, _ := c.Identity()
		return c.String(http.StatusOK, "hello "+user)
	}, a.Auth(func(r *http.Request) (string, bool) {
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
