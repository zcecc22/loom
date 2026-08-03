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
	a.mux.Handle(routed, h)
}
