package gwt

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerbHelpersRouteToCorrectHandlerAndPathValue(t *testing.T) {
	a := New()

	verbs := map[string]func(string, http.HandlerFunc, ...Middleware){
		http.MethodGet:    a.Get,
		http.MethodPost:   a.Post,
		http.MethodPut:    a.Put,
		http.MethodPatch:  a.Patch,
		http.MethodDelete: a.Delete,
	}

	for method, register := range verbs {
		method, register := method, register
		var gotID string
		register("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
			gotID = r.PathValue("id")
			w.WriteHeader(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/items/42", nil)
		a.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s /items/{id}: status = %d, want 200", method, rec.Code)
		}
		if gotID != "42" {
			t.Fatalf("%s /items/{id}: PathValue(id) = %q, want 42", method, gotID)
		}
	}
}

func TestHandleAnyVerbWhenMethodEmpty(t *testing.T) {
	a := New()
	a.Handle("", "/any", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(method, "/any", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s /any: status = %d, want 200", method, rec.Code)
		}
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

	a := New(AppConfig{Logger: discardLogger()})
	a.Use(tag("used"))
	a.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
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
