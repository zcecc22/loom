package gwt

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCtxErrorUsesStandardTextByDefault(t *testing.T) {
	err := (&Ctx{}).Error(http.StatusNotFound)

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("Error did not produce an *HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusNotFound {
		t.Fatalf("Code = %d, want 404", httpErr.Code)
	}
	if httpErr.Message != "Not Found" {
		t.Fatalf("Message = %q, want the standard text", httpErr.Message)
	}
}

func TestCtxErrorCustomMessageAndUnknownCode(t *testing.T) {
	var httpErr *HTTPError
	if !errors.As((&Ctx{}).Error(http.StatusNotFound, "no such item"), &httpErr) {
		t.Fatal("Error did not produce an *HTTPError")
	}
	if httpErr.Message != "no such item" {
		t.Fatalf("Message = %q, want the custom message", httpErr.Message)
	}

	// A code net/http has no text for must still yield a usable message.
	var unknown *HTTPError
	if !errors.As((&Ctx{}).Error(799), &unknown) {
		t.Fatal("Error did not produce an *HTTPError for an unknown code")
	}
	if unknown.Message == "" {
		t.Fatal("Message is empty for a status code net/http does not know")
	}
}

func TestCtxErrorCauseWrapsCauseAndUnwraps(t *testing.T) {
	cause := errors.New("row not found")
	err := (&Ctx{}).ErrorCause(http.StatusNotFound, cause)

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is could not see through the HTTPError to its cause")
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatal("ErrorCause did not produce an *HTTPError")
	}
	if httpErr.Message != "Not Found" {
		t.Fatalf("Message = %q, want the standard text (the cause must not become the message)", httpErr.Message)
	}
	if !strings.Contains(err.Error(), "row not found") {
		t.Fatalf("Error() = %q, want it to mention the cause", err.Error())
	}
}

func TestHTTPErrorSuppliesStatusAndMessage(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	rec := serve(t, a, func(c *Ctx) error {
		return c.Error(http.StatusNotFound, "no such item")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"no such item"}` {
		t.Fatalf("body = %s, want the HTTPError message", got)
	}
}

func TestHTTPErrorFoundAnywhereInTheChain(t *testing.T) {
	// A handler that wraps an *HTTPError with extra context must still get
	// that status, since errors.As walks the chain.
	a := New(AppConfig{Logger: discardLogger()})
	rec := serve(t, a, func(c *Ctx) error {
		return fmt.Errorf("loading item: %w", c.Error(http.StatusNotFound, "no such item"))
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a wrapped HTTPError", rec.Code)
	}
}

func TestPlainErrorBecomes500AndNeverLeaksTheCause(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	rec := serve(t, a, func(c *Ctx) error {
		return errors.New("connection string: user:hunter2@db")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("an internal error leaked into the response body: %s", rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"Internal Server Error"}` {
		t.Fatalf("body = %s, want the generic message", got)
	}
	if !strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("the real cause was not logged, so it is lost entirely: %s", buf.String())
	}
}

func TestClientErrorsLogAtWarnAndServerErrorsAtError(t *testing.T) {
	// A 4xx is the caller's fault; logging it at Error would let anyone
	// fill the error log by typing a bad URL.
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	serve(t, a, func(c *Ctx) error {
		return c.Error(http.StatusNotFound, "nope")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if !strings.Contains(buf.String(), `"level":"WARN"`) {
		t.Fatalf("a 404 was not logged at WARN: %s", buf.String())
	}

	var buf2 bytes.Buffer
	b := New(AppConfig{Logger: jsonLogger(&buf2)})
	serve(t, b, func(c *Ctx) error {
		return errors.New("boom")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if !strings.Contains(buf2.String(), `"level":"ERROR"`) {
		t.Fatalf("a 500 was not logged at ERROR: %s", buf2.String())
	}
}

// TestErrorAfterResponseCommittedDoesNotCorruptIt covers the case the
// `return c.JSON(...)` idiom makes reachable: the handler responded in
// full, then still returned an error. Writing a second status onto that
// response would corrupt it, so the error handler must only log.
func TestErrorAfterResponseCommittedDoesNotCorruptIt(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})

	rec := serve(t, a, func(c *Ctx) error {
		if err := c.JSON(http.StatusOK, H{"ok": true}); err != nil {
			return err
		}
		// e.g. a deferred cleanup that failed after the response went out.
		return errors.New("post-response failure")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the 200 the handler already committed", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"ok":true}` {
		t.Fatalf("body = %s, want the committed payload with nothing appended", got)
	}
	if !strings.Contains(buf.String(), "post-response failure") {
		t.Fatalf("the post-response error was not logged: %s", buf.String())
	}
}

func TestCustomErrorHandlerReplacesTheDefault(t *testing.T) {
	a := New(AppConfig{
		Logger: discardLogger(),
		ErrorHandler: func(c *Ctx, err error) {
			if c.Written() {
				return
			}
			c.SetHeader("X-Handled", "custom")
			_ = c.Stringf(http.StatusTeapot, "custom: %v", err)
		},
	})

	rec := serve(t, a, func(c *Ctx) error {
		return errors.New("boom")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418 from the custom handler", rec.Code)
	}
	if got := rec.Header().Get("X-Handled"); got != "custom" {
		t.Fatalf("X-Handled = %q, want custom", got)
	}
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("body = %q, want the custom handler's rendering", rec.Body.String())
	}
}

func TestNilErrorLeavesTheResponseAlone(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	rec := serve(t, a, func(c *Ctx) error {
		return c.JSON(http.StatusAccepted, H{"ok": true})
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"ok":true}` {
		t.Fatalf("body = %s, want exactly the handler's payload", got)
	}
}

// TestPanicAfterResponseCommittedDoesNotCorruptIt is the panic-path twin
// of the test above. Recover renders through serverError rather than the
// Ctx error handler, so it needs its own committed-response guard —
// without one a handler that responds and then panics emits a valid body
// with a second JSON object appended to it.
func TestPanicAfterResponseCommittedDoesNotCorruptIt(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})

	rec := serve(t, a, func(c *Ctx) error {
		if err := c.JSON(http.StatusOK, H{"ok": true}); err != nil {
			return err
		}
		panic("after the response went out")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the 200 the handler already committed", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"ok":true}` {
		t.Fatalf("body = %s, want the committed payload with nothing appended", got)
	}
	if !strings.Contains(buf.String(), "after the response went out") {
		t.Fatalf("the panic was not logged: %s", buf.String())
	}
}

// TestEveryErrorPathRendersTheSameShape is the guarantee that motivated
// writeJSONError: a client should be able to parse {"error": ...} from any
// error gwt itself produces, without special-casing which layer generated
// it. The four paths reach the wire through different code — Ctx.JSON, the
// unexported writeJSONError from three separate callers — so only an
// end-to-end assertion holds them together.
func TestEveryErrorPathRendersTheSameShape(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	a.GET("/handler-error", func(c *Ctx) error {
		return c.Error(http.StatusNotFound, "no such item")
	})
	a.GET("/panic", func(c *Ctx) error {
		panic("boom")
	})
	a.GET("/guarded", func(c *Ctx) error {
		return nil
	}, a.Authentication(func(r *http.Request) (string, bool) { return "", false }))
	a.POST("/csrf-protected", func(c *Ctx) error {
		return c.NoContent(http.StatusOK)
	}, a.CSRF(CSRFConfig{Secret: []byte("test-secret")}))

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"handler returned an error", http.MethodGet, "/handler-error", http.StatusNotFound, `{"error":"no such item"}`},
		{"handler panicked", http.MethodGet, "/panic", http.StatusInternalServerError, `{"error":"Internal Server Error"}`},
		{"Authentication rejected", http.MethodGet, "/guarded", http.StatusUnauthorized, `{"error":"Unauthorized"}`},
		{"mux found no route", http.MethodGet, "/nothing-here", http.StatusNotFound, `{"error":"Not Found"}`},
		{"mux found no method", http.MethodPost, "/handler-error", http.StatusMethodNotAllowed, `{"error":"Method Not Allowed"}`},
		{"CSRF rejected", http.MethodPost, "/csrf-protected", http.StatusForbidden, `{"error":"Forbidden"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.wantBody {
				t.Fatalf("body = %s, want %s", got, tc.wantBody)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json — the whole point is one parseable shape", ct)
			}
		})
	}
}

func TestMethodNotAllowedKeepsStdlibAllowHeader(t *testing.T) {
	// Rewriting the body must not cost the Allow header stdlib computed;
	// it is the only machine-readable part of a 405.
	a := New(AppConfig{Logger: discardLogger()})
	a.GET("/items/{id}", func(c *Ctx) error { return c.NoContent(http.StatusOK) })
	a.DELETE("/items/{id}", func(c *Ctx) error { return c.NoContent(http.StatusNoContent) })

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/items/42", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	allow := rec.Header().Get("Allow")
	for _, want := range []string{"GET", "DELETE"} {
		if !strings.Contains(allow, want) {
			t.Fatalf("Allow = %q, want it to list %s", allow, want)
		}
	}
}

// TestRedirectsAreNotRewrittenAsNotFound guards the sharp edge of the
// pattern-lookup approach: stdlib answers some requests with a redirect
// rather than a match, and mistaking those for "no route" would silently
// turn every trailing-slash redirect into a 404.
func TestRedirectsAreNotRewrittenAsNotFound(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	a.GET("/dir/", func(c *Ctx) error { return c.String(http.StatusOK, "dir") })
	a.GET("/items/{id}", func(c *Ctx) error { return c.String(http.StatusOK, "item") })

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantLoc    string
	}{
		{"trailing-slash redirect", "/dir", http.StatusTemporaryRedirect, "/dir/"},
		{"path cleaning redirect", "//dir//x", http.StatusTemporaryRedirect, "/dir/x"},
		{"ordinary subtree match", "/dir/", http.StatusOK, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := rec.Header().Get("Location"); got != tc.wantLoc {
				t.Fatalf("Location = %q, want %q", got, tc.wantLoc)
			}
		})
	}
}

// TestPathValuesSurviveTheNotFoundWrapper is the regression guard for the
// reason muxErrorsAsJSON only *asks* the mux which pattern matches instead
// of serving the handler it returns: that shortcut does not populate path
// wildcards, and Ctx.Param would silently come back empty.
func TestPathValuesSurviveTheNotFoundWrapper(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	var got string
	a.GET("/items/{id}/sub/{rest...}", func(c *Ctx) error {
		got = c.Param("id") + "|" + c.Param("rest")
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/42/sub/a/b", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got != "42|a/b" {
		t.Fatalf("Param values = %q, want %q", got, "42|a/b")
	}
}

// TestHandleHTTPErrorsAreLeftAlone documents the deliberate limit: a raw
// net/http handler mounted with HandleHTTP owns its output, JSON or not.
// ServeStatic is built on HandleHTTP, so a missing asset still 404s the way
// http.FileServer does.
func TestHandleHTTPErrorsAreLeftAlone(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	a.HandleHTTP(http.MethodGet, "/raw", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "raw plain text", http.StatusNotFound)
	}))

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/raw", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "raw plain text" {
		t.Fatalf("body = %q, want the raw handler's own output untouched", got)
	}
}
