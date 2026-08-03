package gwt

import "net/http"

// GET registers h for GET requests matching pattern (stdlib ServeMux
// syntax, e.g. "/items/{id}" — read wildcards with Ctx.Param), wrapped by
// mw (route-level, innermost — closest to h).
//
// Per stdlib ServeMux, a GET pattern also matches HEAD requests.
func (a *App) GET(pattern string, h Handler, mw ...Middleware) {
	a.Handle(http.MethodGet, pattern, h, mw...)
}

// POST registers h for POST requests matching pattern. See GET.
func (a *App) POST(pattern string, h Handler, mw ...Middleware) {
	a.Handle(http.MethodPost, pattern, h, mw...)
}

// PUT registers h for PUT requests matching pattern. See GET.
func (a *App) PUT(pattern string, h Handler, mw ...Middleware) {
	a.Handle(http.MethodPut, pattern, h, mw...)
}

// PATCH registers h for PATCH requests matching pattern. See GET.
func (a *App) PATCH(pattern string, h Handler, mw ...Middleware) {
	a.Handle(http.MethodPatch, pattern, h, mw...)
}

// DELETE registers h for DELETE requests matching pattern. See GET.
func (a *App) DELETE(pattern string, h Handler, mw ...Middleware) {
	a.Handle(http.MethodDelete, pattern, h, mw...)
}

// Handle registers h for pattern under method, using stdlib ServeMux's
// "METHOD /path" pattern syntax. If method is empty, the handler matches
// any verb (stdlib's bare-pattern behavior). This is the generic
// counterpart to GET/POST/PUT/PATCH/DELETE, for verbs without a dedicated
// method (e.g. OPTIONS) or when the verb is only known at runtime. mw
// wraps h only, not the whole App — see Use for whole-app middleware.
func (a *App) Handle(method, pattern string, h Handler, mw ...Middleware) {
	// The Ctx is built here, inside the route's own handler, so it exists
	// only for requests that actually matched a route. Middleware stays
	// plain net/http (see the Middleware type) and therefore also covers
	// the mux's own 404/405 responses, which have no route and no Ctx.
	a.HandleHTTP(method, pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := newCtx(a, w, r)
		if err := h(c); err != nil {
			a.handleError(c, err)
		}
	}), mw...)
}

// muxErrorsAsJSON wraps the mux so the error responses it generates itself
// — the plain-text "404 page not found" and "Method Not Allowed" — come out
// in gwt's JSON error shape like everything else. Handler installs it as
// the innermost global wrapper, so Logging still reports the real status
// and Recover still covers it.
//
// It works by elimination rather than by asking the mux anything: every
// registered route steps out of the notFoundWriter before running (see
// HandleHTTP), so whatever is still writing through it is the mux's own
// response. That keeps the cost to one small wrapper per request.
//
// Being innermost is what makes that reliable rather than best-effort:
// this wrapper is applied after the whole global chain, so the mux always
// receives exactly a *notFoundWriter no matter what Use-added middleware
// wraps around it, and HandleHTTP's type assertion cannot miss.
//
// The tempting alternative — asking ServeMux.Handler which pattern matches
// — costs a second route lookup on every request, measurably as expensive
// as the entire first one, to benefit only the responses that 404.
func (a *App) muxErrorsAsJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&notFoundWriter{ResponseWriter: w, app: a, req: r}, r)
	})
}

// notFoundWriter replaces the body of any error response written through it
// with gwt's JSON error shape. Only the mux's own responses reach it —
// registered routes unwrap it first — so it never second-guesses a
// handler's output.
//
// Statuses below 400 pass through untouched, which is what keeps stdlib's
// trailing-slash and path-cleaning redirects (301) working.
//
// It deliberately forwards only Unwrap, not Flusher/Hijacker/ReaderFrom
// like statusWriter does: the only things that ever write through it are
// stdlib's fixed one-line not-found and redirect handlers, and nothing
// streams or hijacks from there.
type notFoundWriter struct {
	http.ResponseWriter
	app *App
	req *http.Request

	wroteHeader bool
	rewriting   bool
}

// Unwrap exposes the underlying writer to http.ResponseController and to
// HandleHTTP's step-out.
func (n *notFoundWriter) Unwrap() http.ResponseWriter {
	return n.ResponseWriter
}

func (n *notFoundWriter) WriteHeader(status int) {
	if n.wroteHeader {
		return
	}
	n.wroteHeader = true

	if status < 400 {
		n.ResponseWriter.WriteHeader(status)
		return
	}

	// No registered route ran, or it would have stepped out of this
	// writer — so this is the mux answering for itself. Write our own body
	// and swallow the plain-text one that follows.
	n.rewriting = true
	if err := writeJSONError(n.ResponseWriter, status, statusMessage(status)); err != nil {
		n.app.logger.Error("gwt: write error response failed", "err", err, "method", n.req.Method, "path", n.req.URL.Path)
	}
}

func (n *notFoundWriter) Write(b []byte) (int, error) {
	if !n.wroteHeader {
		n.WriteHeader(http.StatusOK)
	}
	if n.rewriting {
		// Report the write as successful so stdlib's handler doesn't treat
		// the suppression as an I/O failure.
		return len(b), nil
	}
	return n.ResponseWriter.Write(b)
}

// HandleHTTP registers a plain http.Handler, bypassing Ctx entirely. It is
// the escape hatch for mounting handlers written against net/http directly
// — a third-party handler, a reverse proxy, an http.FileServer. Pattern,
// method, and mw behave exactly as in Handle.
func (a *App) HandleHTTP(method, pattern string, h http.Handler, mw ...Middleware) {
	if len(mw) > 0 {
		h = Chain(mw...)(h)
	}
	routed := pattern
	if method != "" {
		routed = method + " " + pattern
	}

	a.mux.Handle(routed, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A registered route owns its response, whatever status it chooses,
		// so step out of the not-found interceptor before running. This is
		// what makes muxErrorsAsJSON's by-elimination rule true — anything
		// still writing through that wrapper is the mux itself — and it
		// keeps the route's writer the fully-featured statusWriter, so
		// streaming and hijacking are unaffected.
		if nf, ok := w.(*notFoundWriter); ok {
			w = nf.ResponseWriter
		}
		h.ServeHTTP(w, r)
	}))
}
