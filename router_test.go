package gwt

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerbHelpersRouteToCorrectHandlerAndPathValue(t *testing.T) {
	a := mustNew(t)

	verbs := map[string]func(string, Handler, ...Middleware){
		http.MethodGet:    a.GET,
		http.MethodPost:   a.POST,
		http.MethodPut:    a.PUT,
		http.MethodPatch:  a.PATCH,
		http.MethodDelete: a.DELETE,
	}

	for method, register := range verbs {
		method, register := method, register
		var gotID string
		register("/items/{id}", func(c *Ctx) error {
			gotID = c.Param("id")
			return c.NoContent(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/items/42", nil)
		a.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s /items/{id}: status = %d, want 200", method, rec.Code)
		}
		if gotID != "42" {
			t.Fatalf("%s /items/{id}: Param(id) = %q, want 42", method, gotID)
		}
	}
}

func TestHandleAnyVerbWhenMethodEmpty(t *testing.T) {
	a := mustNew(t)
	a.Handle("", "/any", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	})

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(method, "/any", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s /any: status = %d, want 200", method, rec.Code)
		}
	}
}

func TestHandleHTTPMountsPlainNetHTTPHandler(t *testing.T) {
	// HandleHTTP is the escape hatch: no Ctx is built, and the handler's own
	// response is what reaches the client. Route middleware still applies.
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.HandleHTTP(http.MethodGet, "/raw", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("raw"))
	}), func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Raw", "1")
			next.ServeHTTP(w, r)
		})
	})

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/raw", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
	if got := rec.Body.String(); got != "raw" {
		t.Fatalf("body = %q, want %q", got, "raw")
	}
	if got := rec.Header().Get("X-Raw"); got != "1" {
		t.Fatalf("X-Raw = %q, want 1 (route middleware should still wrap HandleHTTP)", got)
	}
}

func TestEndToEndDefaultAndUseMiddlewareOrder(t *testing.T) {
	var order []string
	tag := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.Use(tag("used"))
	a.GET("/items/{id}", func(c *Ctx) error {
		order = append(order, "handler")
		return c.NoContent(http.StatusOK)
	}, tag("route"))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/7", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Logging and Recover (New's unconditional defaults) run before "used"
	// but aren't directly observable as tags here; what's asserted is that
	// everything downstream of them still runs in the documented order:
	// Use-added middleware, then route-level middleware, then the handler.
	want := []string{"used", "route", "handler"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
