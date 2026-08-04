package gwt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// HTTPError is an error carrying an HTTP status code and a message safe to
// send to the client. Returning one from a Handler produces exactly that
// status and message; returning any other error produces a 500 whose
// details stay in the log.
//
// Err optionally carries the underlying cause. It is logged, never sent —
// so wrapping an internal error is safe:
//
//	return c.ErrorCause(404, err)
type HTTPError struct {
	// Code is the HTTP status code to respond with.
	Code int
	// Message is the client-visible explanation.
	Message string
	// Err is the underlying cause, if any. Logged, never sent.
	Err error
}

// Error implements error.
func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("gwt: %d %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("gwt: %d %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause, so errors.Is and errors.As see
// through an HTTPError to whatever it wraps.
func (e *HTTPError) Unwrap() error {
	return e.Err
}

// Error builds an *HTTPError with the given status code. message is
// optional and defaults to the standard text for code:
//
//	return c.Error(404)                  // "Not Found"
//	return c.Error(404, "no such item")  // custom
//
// Error is a Ctx method, like every other response-related helper
// (JSON, HTML, String, ...), even though it doesn't touch c.W/c.R
// itself — every real call site already has a c in scope, since it's
// only ever called from inside a Handler.
func (c *Ctx) Error(code int, message ...string) error {
	msg := statusMessage(code)
	if len(message) > 0 {
		msg = message[0]
	}
	return &HTTPError{Code: code, Message: msg}
}

// ErrorCause builds an *HTTPError with the given status code wrapping err.
// The client sees only the standard text for code; err is logged.
func (c *Ctx) ErrorCause(code int, err error) error {
	return &HTTPError{Code: code, Message: statusMessage(code), Err: err}
}

// statusMessage returns the standard text for a status code, falling back
// to a generic word for codes net/http doesn't know.
func statusMessage(code int) string {
	if text := http.StatusText(code); text != "" {
		return text
	}
	return "Error"
}

// writeJSONError writes gwt's canonical error body — {"error":"<message>"} —
// with the given status. It is the shared writer for the error paths that
// have no Ctx to render through: Recover's 500, Authentication's 401,
// CSRF's 403, and the mux's own 404/405. defaultErrorHandler produces the
// identical shape via Ctx.JSON, and TestEveryErrorPathRendersTheSameShape
// holds the two together.
func writeJSONError(w http.ResponseWriter, status int, message string) error {
	body, err := json.Marshal(H{"error": message})
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, werr := w.Write(body)
	return werr
}

// handleError renders a non-nil error returned by a Handler, using
// AppConfig.ErrorHandler if one was set and defaultErrorHandler otherwise.
func (a *App) handleError(c *Ctx, err error) {
	if a.errorHandler != nil {
		a.errorHandler(c, err)
		return
	}
	a.defaultErrorHandler(c, err)
}

// defaultErrorHandler logs err and writes a JSON error response.
//
// An *HTTPError anywhere in err's chain supplies the status and the
// client-visible message. Anything else is a 500 whose body says only
// "Internal Server Error" — the real error goes to the log and never to
// the client, so an internal failure can't leak a query, a path, or a
// stack detail into a response body.
func (a *App) defaultErrorHandler(c *Ctx, err error) {
	code := http.StatusInternalServerError
	message := statusMessage(http.StatusInternalServerError)

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		code = httpErr.Code
		message = httpErr.Message
	}

	// A 5xx is the app's fault and worth an Error; a 4xx is the caller's
	// and would otherwise let anyone fill the log at Error level.
	logAt := a.logger.Warn
	if code >= http.StatusInternalServerError {
		logAt = a.logger.Error
	}
	logAt("gwt: request failed",
		"err", err,
		"status", code,
		"method", c.R.Method,
		"path", c.R.URL.Path,
	)

	// A handler may respond in full and then fail — `return c.JSON(...)`
	// surfaces a write error after the body is already on the wire, and a
	// deferred cleanup can fail later still. Writing a second status here
	// would corrupt that response, so once committed, only log.
	if c.Written() {
		return
	}

	if werr := c.JSON(code, H{"error": message}); werr != nil {
		a.logger.Error("gwt: write error response failed", "err", werr, "method", c.R.Method, "path", c.R.URL.Path)
	}
}
