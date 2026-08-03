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

func TestStatusUsesStandardTextByDefault(t *testing.T) {
	err := Status(http.StatusNotFound)

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("Status did not produce an *HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusNotFound {
		t.Fatalf("Code = %d, want 404", httpErr.Code)
	}
	if httpErr.Message != "Not Found" {
		t.Fatalf("Message = %q, want the standard text", httpErr.Message)
	}
}

func TestStatusCustomMessageAndUnknownCode(t *testing.T) {
	var httpErr *HTTPError
	if !errors.As(Status(http.StatusNotFound, "no such item"), &httpErr) {
		t.Fatal("Status did not produce an *HTTPError")
	}
	if httpErr.Message != "no such item" {
		t.Fatalf("Message = %q, want the custom message", httpErr.Message)
	}

	// A code net/http has no text for must still yield a usable message.
	var unknown *HTTPError
	if !errors.As(Status(799), &unknown) {
		t.Fatal("Status did not produce an *HTTPError for an unknown code")
	}
	if unknown.Message == "" {
		t.Fatal("Message is empty for a status code net/http does not know")
	}
}

func TestStatusErrWrapsCauseAndUnwraps(t *testing.T) {
	cause := errors.New("row not found")
	err := StatusErr(http.StatusNotFound, cause)

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is could not see through the HTTPError to its cause")
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatal("StatusErr did not produce an *HTTPError")
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
		return Status(http.StatusNotFound, "no such item")
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
		return fmt.Errorf("loading item: %w", Status(http.StatusNotFound, "no such item"))
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
		return Status(http.StatusNotFound, "nope")
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
