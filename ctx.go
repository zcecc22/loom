package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// H is shorthand for a JSON object literal: loom.H{"hello": "world"}
// marshals to {"hello":"world"}. It is an ordinary map, so anything a
// map[string]any accepts works here too.
type H map[string]any

// Handler is a loom request handler. Returning a non-nil error hands the
// response over to the App's error handler, which is what removes the
// per-failure "write a 500 and return" boilerplate from handler bodies:
//
//	app.GET("/items/{id}", func(c *loom.Ctx) error {
//		item, err := store.Find(c.Param("id"))
//		if err != nil {
//			return c.Error(404, "no such item")
//		}
//		return c.JSON(200, item)
//	})
//
// See HTTPError and Ctx.Error for controlling the status code a returned
// error produces; any other error becomes a 500.
type Handler func(*Ctx) error

// Ctx carries everything a handler needs for one request: the underlying
// ResponseWriter and Request, plus helpers for reading the request and
// writing a response.
//
// W and R are exported deliberately — Ctx is a convenience layer over
// net/http, never a wall around it. Anything Ctx doesn't cover is one
// field access away, and handing c.W/c.R to an ordinary http.Handler
// always works.
type Ctx struct {
	// W is the underlying http.ResponseWriter.
	W http.ResponseWriter
	// R is the underlying *http.Request.
	R *http.Request

	app *App
	// wroteHeader records whether a response has been committed, so the
	// error handler can tell "handler failed before responding" from
	// "handler responded, then failed" — see App.handleError.
	wroteHeader bool
}

// newCtx builds the per-request Ctx. It is called once per matched route,
// by the adapter in Handle.
func newCtx(a *App, w http.ResponseWriter, r *http.Request) *Ctx {
	return &Ctx{W: w, R: r, app: a}
}

// --- request ---

// Param returns the path wildcard named by name, using stdlib ServeMux
// syntax: a route registered as "/items/{id}" reads c.Param("id"). It
// returns "" if the route has no such wildcard.
func (c *Ctx) Param(name string) string {
	return c.R.PathValue(name)
}

// Query returns the named URL query parameter, or "" if absent.
func (c *Ctx) Query(name string) string {
	return c.R.URL.Query().Get(name)
}

// QueryDefault returns the named URL query parameter, or def if it is
// absent or empty.
func (c *Ctx) QueryDefault(name, def string) string {
	if v := c.Query(name); v != "" {
		return v
	}
	return def
}

// FormValue returns the named form value, parsing the request body as a
// form if needed. It is stdlib's r.FormValue, which consults the URL query
// as well as the body.
func (c *Ctx) FormValue(name string) string {
	return c.R.FormValue(name)
}

// GetHeader returns the named request header, or "" if absent.
func (c *Ctx) GetHeader(name string) string {
	return c.R.Header.Get(name)
}

// Bind decodes the request body as JSON into v, which must be a pointer.
// A malformed body produces an HTTPError with status 400, so a handler can
// simply return it:
//
//	var in Item
//	if err := c.Bind(&in); err != nil {
//		return err // -> 400
//	}
//
// Bind does not limit the body's size. Use the MaxBodySize middleware for
// that.
func (c *Ctx) Bind(v any) error {
	if err := json.NewDecoder(c.R.Body).Decode(v); err != nil {
		return &HTTPError{Code: http.StatusBadRequest, Message: "invalid JSON body", Err: err}
	}
	return nil
}

// Context returns the request's context.
func (c *Ctx) Context() context.Context {
	return c.R.Context()
}

// Value returns the value the request's context carries for key, or nil.
// It is the read side for middleware that stashed something with
// context.WithValue — including middleware written against plain net/http,
// which is why key is an any rather than a loom-specific type.
func (c *Ctx) Value(key any) any {
	return c.R.Context().Value(key)
}

// Identity returns the identity an Authentication middleware stashed for
// this request, and whether one was set. It is the handler-side
// equivalent of the package-level Identity function.
func (c *Ctx) Identity() (identity string, ok bool) {
	return Identity(c.R)
}

// CSRFToken returns the current valid CSRF token an App.CSRF middleware
// set for this request, and whether one was set. Embed it in a hidden
// form field or a meta tag so a subsequent unsafe request can echo it
// back. It is the handler-side equivalent of the package-level CSRFToken
// function.
func (c *Ctx) CSRFToken() (token string, ok bool) {
	return CSRFToken(c.R)
}

// Logger returns the App's logger.
func (c *Ctx) Logger() *slog.Logger {
	return c.app.logger
}

// --- response ---

// SetHeader sets a response header. It must be called before the response
// is written, as headers are flushed with the status line.
func (c *Ctx) SetHeader(key, value string) {
	c.W.Header().Set(key, value)
}

// Written reports whether a response has already been committed — that is,
// whether any of JSON, String, HTML, NoContent, or Redirect has run. A
// custom AppConfig.ErrorHandler should check this before writing, since
// writing a second status onto a committed response corrupts it.
func (c *Ctx) Written() bool {
	return c.wroteHeader
}

// write commits the response: content type, status, then body. Every
// response helper funnels through here so wroteHeader is set in exactly
// one place.
func (c *Ctx) write(status int, contentType string, body []byte) error {
	c.W.Header().Set("Content-Type", contentType)
	c.W.WriteHeader(status)
	c.wroteHeader = true
	if _, err := c.W.Write(body); err != nil {
		return fmt.Errorf("loom: write response: %w", err)
	}
	return nil
}

// JSON marshals data and writes it with the given status code. If
// marshaling fails, nothing is written to the response — the error is
// returned instead, so the error handler renders a clean 500 rather than a
// half-serialized body.
func (c *Ctx) JSON(status int, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("loom: marshal json response: %w", err)
	}
	return c.write(status, "application/json; charset=utf-8", body)
}

// String writes s as a plain-text response, literally — s is never treated
// as a format string, so text containing a stray % is safe. Use Stringf to
// format.
func (c *Ctx) String(status int, s string) error {
	return c.write(status, "text/plain; charset=utf-8", []byte(s))
}

// Stringf formats its arguments and writes the result as a plain-text
// response. The f suffix is what lets go vet check the format string
// against its arguments, the same way it checks fmt.Printf.
func (c *Ctx) Stringf(status int, format string, args ...any) error {
	return c.write(status, "text/plain; charset=utf-8", []byte(fmt.Sprintf(format, args...)))
}

// HTML executes the named template from the App's template set against
// data and writes the result with the given status code. Load the set with
// App.LoadTemplates or AppConfig.Templates first; name is the template's
// name within that set, typically its base filename ("index.tmpl").
//
// The template is rendered fully into memory before anything is written to
// the response, so an execute failure never produces a partial body — the
// error is returned instead and the error handler renders a 500.
func (c *Ctx) HTML(status int, name string, data any) error {
	if c.app.templates == nil {
		return fmt.Errorf("loom: cannot render %q: no templates loaded (call App.LoadTemplates or set AppConfig.Templates)", name)
	}

	var buf bytes.Buffer
	if err := c.app.templates.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("loom: execute template %q: %w", name, err)
	}

	return c.write(status, "text/html; charset=utf-8", buf.Bytes())
}

// NoContent writes status with an empty body.
func (c *Ctx) NoContent(status int) error {
	c.W.WriteHeader(status)
	c.wroteHeader = true
	return nil
}

// Redirect replies with a redirect to url using the given 3xx status code.
func (c *Ctx) Redirect(status int, url string) error {
	http.Redirect(c.W, c.R, url, status)
	c.wroteHeader = true
	return nil
}
