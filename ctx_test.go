package gwt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// serve registers h at GET /t and runs one request against it, returning
// the recorder. It keeps the Ctx tests to their actual subject.
func serve(t *testing.T, a *App, h Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	a.GET("/t", h)
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, req)
	return rec
}

func TestCtxJSONWritesBodyStatusAndContentType(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	rec := serve(t, a, func(c *Ctx) error {
		return c.JSON(http.StatusCreated, H{"name": "widget"})
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json prefix", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body not valid JSON: %v (%s)", err, rec.Body.String())
	}
	if got["name"] != "widget" {
		t.Fatalf("body = %v, want name=widget", got)
	}
}

func TestCtxJSONMarshalErrorWritesNothingAndBecomes500(t *testing.T) {
	var buf bytes.Buffer
	a := mustNew(t, AppConfig{Logger: jsonLogger(&buf)})
	// A channel value cannot be marshaled to JSON. Nothing must reach the
	// response before the failure is known, so the error handler gets a
	// clean writer to render a 500 onto.
	rec := serve(t, a, func(c *Ctx) error {
		return c.JSON(http.StatusOK, H{"ch": make(chan int)})
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on marshal error", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "chan") {
		t.Fatalf("marshal failure leaked into the body: %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "request failed") {
		t.Fatalf("expected the marshal error to be logged, got: %s", buf.String())
	}
}

func TestCtxStringAndStringf(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	rec := serve(t, a, func(c *Ctx) error {
		return c.Stringf(http.StatusOK, "hello, %s", "world")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "hello, world" {
		t.Fatalf("body = %q, want %q", got, "hello, world")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain prefix", ct)
	}

	// String writes literally, so a stray % in user-supplied text can't
	// turn into a format verb.
	b := mustNew(t, AppConfig{Logger: discardLogger()})
	rec2 := serve(t, b, func(c *Ctx) error {
		return c.String(http.StatusOK, "100% done")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))
	if got := rec2.Body.String(); got != "100% done" {
		t.Fatalf("body = %q, want %q", got, "100% done")
	}
}

func TestCtxHTMLEscapesXSS(t *testing.T) {
	dir := templateDirFS(t, map[string]string{"page.tmpl": `<p>{{.Name}}</p>`})
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	if err := a.LoadTemplates(os.DirFS(dir)); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	rec := serve(t, a, func(c *Ctx) error {
		return c.HTML(http.StatusOK, "page.tmpl", struct{ Name string }{Name: `<script>alert(1)</script>`})
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("XSS payload was not escaped: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag, got: %s", body)
	}
}

func TestCtxHTMLExecuteErrorWritesNothingThen500(t *testing.T) {
	// {{.Missing.Field}} on a struct without that field is a runtime
	// execute error. Because HTML buffers first, the response is untouched
	// when it fails — no partial body ahead of the 500.
	dir := templateDirFS(t, map[string]string{"bad.tmpl": `prefix {{.Missing.Field}}`})
	var buf bytes.Buffer
	a := mustNew(t, AppConfig{Logger: jsonLogger(&buf)})
	if err := a.LoadTemplates(os.DirFS(dir)); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	rec := serve(t, a, func(c *Ctx) error {
		return c.HTML(http.StatusOK, "bad.tmpl", struct{ Name string }{Name: "x"})
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on execute error", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "prefix") {
		t.Fatalf("a partial body was written before the execute error: %s", rec.Body.String())
	}
}

func TestCtxHTMLWithNoTemplatesLoadedIsCleanError(t *testing.T) {
	// No LoadTemplates call and no AppConfig.Templates: rendering must
	// report a clear error rather than dereferencing a nil template set.
	var buf bytes.Buffer
	a := mustNew(t, AppConfig{Logger: jsonLogger(&buf)})

	rec := serve(t, a, func(c *Ctx) error {
		return c.HTML(http.StatusOK, "index.tmpl", nil)
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "no templates loaded") {
		t.Fatalf("expected an explanatory log entry, got: %s", buf.String())
	}
}

func TestCtxHTMLUnknownTemplateNameIsError(t *testing.T) {
	dir := templateDirFS(t, map[string]string{"page.tmpl": `<p>ok</p>`})
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	if err := a.LoadTemplates(os.DirFS(dir)); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	rec := serve(t, a, func(c *Ctx) error {
		return c.HTML(http.StatusOK, "nope.tmpl", nil)
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for an unknown template name", rec.Code)
	}
}

func TestCtxNoContentAndRedirect(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	rec := serve(t, a, func(c *Ctx) error {
		return c.NoContent(http.StatusNoContent)
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("NoContent wrote a body: %q", rec.Body.String())
	}

	b := mustNew(t, AppConfig{Logger: discardLogger()})
	rec2 := serve(t, b, func(c *Ctx) error {
		return c.Redirect(http.StatusFound, "/elsewhere")
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if rec2.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec2.Code)
	}
	if got := rec2.Header().Get("Location"); got != "/elsewhere" {
		t.Fatalf("Location = %q, want /elsewhere", got)
	}
}

func TestCtxRequestHelpers(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var gotID, gotQuery, gotDefault, gotHeader string

	a.GET("/items/{id}", func(c *Ctx) error {
		gotID = c.Param("id")
		gotQuery = c.Query("format")
		gotDefault = c.QueryDefault("missing", "fallback")
		gotHeader = c.GetHeader("X-Test")
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/items/42?format=short", nil)
	req.Header.Set("X-Test", "present")
	a.ServeHTTP(httptest.NewRecorder(), req)

	if gotID != "42" {
		t.Fatalf("Param(id) = %q, want 42", gotID)
	}
	if gotQuery != "short" {
		t.Fatalf("Query(format) = %q, want short", gotQuery)
	}
	if gotDefault != "fallback" {
		t.Fatalf("QueryDefault(missing) = %q, want fallback", gotDefault)
	}
	if gotHeader != "present" {
		t.Fatalf("GetHeader(X-Test) = %q, want present", gotHeader)
	}
}

func TestCtxQueryDefaultPrefersPresentValue(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var got string
	serve(t, a, func(c *Ctx) error {
		got = c.QueryDefault("format", "fallback")
		return c.NoContent(http.StatusOK)
	}, httptest.NewRequest(http.MethodGet, "/t?format=short", nil))

	if got != "short" {
		t.Fatalf("QueryDefault = %q, want short", got)
	}
}

func TestCtxFormValueReadsBodyAndQuery(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var fromBody, fromQuery, missing string

	a.POST("/t", func(c *Ctx) error {
		fromBody = c.FormValue("name")
		fromQuery = c.FormValue("mode")
		missing = c.FormValue("absent")
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/t?mode=fast", strings.NewReader("name=widget"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.ServeHTTP(httptest.NewRecorder(), req)

	if fromBody != "widget" {
		t.Fatalf("FormValue(name) = %q, want widget (from the form body)", fromBody)
	}
	// Stdlib's FormValue consults the query string too, and gwt does not
	// change that.
	if fromQuery != "fast" {
		t.Fatalf("FormValue(mode) = %q, want fast (from the query string)", fromQuery)
	}
	if missing != "" {
		t.Fatalf("FormValue(absent) = %q, want empty", missing)
	}
}

func TestCtxBindDecodesJSON(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	type item struct {
		Name string `json:"name"`
	}
	var got item

	rec := serve(t, a, func(c *Ctx) error {
		if err := c.Bind(&got); err != nil {
			return err
		}
		return c.NoContent(http.StatusOK)
	}, httptest.NewRequest(http.MethodGet, "/t", strings.NewReader(`{"name":"widget"}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.Name != "widget" {
		t.Fatalf("decoded = %+v, want name=widget", got)
	}
}

func TestCtxBindMalformedBodyIs400NotA500(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	rec := serve(t, a, func(c *Ctx) error {
		var v struct{ Name string }
		if err := c.Bind(&v); err != nil {
			return err
		}
		return c.NoContent(http.StatusOK)
	}, httptest.NewRequest(http.MethodGet, "/t", strings.NewReader("{not json")))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — a malformed body is the client's fault", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid JSON body") {
		t.Fatalf("body = %q, want the Bind message", rec.Body.String())
	}
}

func TestCtxValueReadsForeignContextKeys(t *testing.T) {
	// Middleware written against plain net/http stashes values under its
	// own key type; Ctx.Value must be able to read them back.
	type foreignKey struct{}
	a := mustNew(t, AppConfig{Logger: discardLogger()})

	var got any
	a.GET("/t", func(c *Ctx) error {
		got = c.Value(foreignKey{})
		return c.NoContent(http.StatusOK)
	}, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), foreignKey{}, "from-middleware")))
		})
	})

	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/t", nil))

	if got != "from-middleware" {
		t.Fatalf("Ctx.Value = %v, want from-middleware", got)
	}
}

func TestCtxIdentityMatchesPackageIdentity(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var viaCtx, viaFunc string
	var okCtx bool

	a.GET("/t", func(c *Ctx) error {
		viaCtx, okCtx = c.Identity()
		viaFunc, _ = Identity(c.R)
		return c.NoContent(http.StatusOK)
	}, a.Authentication(func(r *http.Request) (string, bool) { return "alice", true }))

	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/t", nil))

	if !okCtx || viaCtx != "alice" {
		t.Fatalf("Ctx.Identity() = %q, %v; want alice, true", viaCtx, okCtx)
	}
	if viaFunc != viaCtx {
		t.Fatalf("Ctx.Identity() = %q but Identity(r) = %q; they must agree", viaCtx, viaFunc)
	}
}

func TestCtxSetHeaderAndLogger(t *testing.T) {
	logger := discardLogger()
	a := mustNew(t, AppConfig{Logger: logger})

	var sameLogger bool
	rec := serve(t, a, func(c *Ctx) error {
		c.SetHeader("X-Custom", "yes")
		sameLogger = c.Logger() == logger
		return c.NoContent(http.StatusOK)
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if got := rec.Header().Get("X-Custom"); got != "yes" {
		t.Fatalf("X-Custom = %q, want yes", got)
	}
	if !sameLogger {
		t.Fatal("Ctx.Logger() did not return the App's logger")
	}
}

func TestCtxWrittenReportsCommitState(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var before, after bool

	serve(t, a, func(c *Ctx) error {
		before = c.Written()
		err := c.JSON(http.StatusOK, H{"ok": true})
		after = c.Written()
		return err
	}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if before {
		t.Fatal("Written() was true before anything was written")
	}
	if !after {
		t.Fatal("Written() was false after JSON committed the response")
	}
}

// TestLoggingForwardsOptionalInterfaces guards the optional interfaces
// through the always-on Logging middleware. statusWriter wraps every
// response, and Logging cannot be removed, so anything it fails to forward
// is unreachable in every gwt app — which is what silently broke SSE and
// WebSocket upgrades before these were added.
func TestLoggingForwardsOptionalInterfaces(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})

	var isFlusher, isHijacker, isReaderFrom, unwrapped bool
	a.GET("/t", func(c *Ctx) error {
		_, isFlusher = c.W.(http.Flusher)
		_, isHijacker = c.W.(http.Hijacker)
		_, isReaderFrom = c.W.(io.ReaderFrom)
		_, unwrapped = c.W.(interface{ Unwrap() http.ResponseWriter })
		return c.NoContent(http.StatusOK)
	})

	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/t", nil))

	for name, ok := range map[string]bool{
		"http.Flusher":  isFlusher,
		"http.Hijacker": isHijacker,
		"io.ReaderFrom": isReaderFrom,
		"Unwrap":        unwrapped,
	} {
		if !ok {
			t.Fatalf("%s did not survive the Logging wrapper", name)
		}
	}
}

func TestLoggingFlushReachesUnderlyingWriter(t *testing.T) {
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.GET("/t", func(c *Ctx) error {
		if err := c.String(http.StatusOK, "chunk"); err != nil {
			return err
		}
		// The streaming idiom: write, then flush. It must reach the real
		// writer, not stop at statusWriter.
		return http.NewResponseController(c.W).Flush()
	})

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t", nil))

	if !rec.Flushed {
		t.Fatal("Flush did not reach the underlying ResponseWriter")
	}
	if rec.Body.String() != "chunk" {
		t.Fatalf("body = %q, want chunk", rec.Body.String())
	}
}

func TestLoggingHijackDelegatesAndReportsUnsupported(t *testing.T) {
	// Over a plain recorder there is no connection to hijack, so the
	// stdlib sentinel is the correct answer — not a panic.
	a := mustNew(t, AppConfig{Logger: discardLogger()})
	var unsupportedErr error
	a.GET("/t", func(c *Ctx) error {
		_, _, unsupportedErr = c.W.(http.Hijacker).Hijack()
		return c.NoContent(http.StatusOK)
	})
	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/t", nil))

	if !errors.Is(unsupportedErr, http.ErrNotSupported) {
		t.Fatalf("Hijack over a non-hijackable writer = %v, want http.ErrNotSupported", unsupportedErr)
	}

	// Over a writer that does support it, the call must be delegated.
	b := mustNew(t, AppConfig{Logger: discardLogger()})
	var delegatedErr error
	b.GET("/t", func(c *Ctx) error {
		_, _, delegatedErr = c.W.(http.Hijacker).Hijack()
		return nil
	})
	b.ServeHTTP(&hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/t", nil))

	if !errors.Is(delegatedErr, errHijacked) {
		t.Fatalf("Hijack was not delegated to the underlying writer: got %v", delegatedErr)
	}
}

// errHijacked is the sentinel hijackableRecorder returns, so the test can
// tell "delegated to the underlying writer" from "handled by statusWriter".
var errHijacked = errors.New("hijacked")

// hijackableRecorder is a recorder that also supports hijacking, standing
// in for a real connection-backed ResponseWriter.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errHijacked
}
