package gwt

import "net/http"

// Get registers h for GET requests matching pattern (stdlib ServeMux
// syntax, e.g. "/items/{id}"), wrapped by mw (route-level, innermost —
// closest to h).
func (a *App) Get(pattern string, h http.HandlerFunc, mw ...Middleware) {
	a.Handle(http.MethodGet, pattern, h, mw...)
}

// Post registers h for POST requests matching pattern. See Get.
func (a *App) Post(pattern string, h http.HandlerFunc, mw ...Middleware) {
	a.Handle(http.MethodPost, pattern, h, mw...)
}

// Put registers h for PUT requests matching pattern. See Get.
func (a *App) Put(pattern string, h http.HandlerFunc, mw ...Middleware) {
	a.Handle(http.MethodPut, pattern, h, mw...)
}

// Patch registers h for PATCH requests matching pattern. See Get.
func (a *App) Patch(pattern string, h http.HandlerFunc, mw ...Middleware) {
	a.Handle(http.MethodPatch, pattern, h, mw...)
}

// Delete registers h for DELETE requests matching pattern. See Get.
func (a *App) Delete(pattern string, h http.HandlerFunc, mw ...Middleware) {
	a.Handle(http.MethodDelete, pattern, h, mw...)
}

// Handle registers h for pattern under method, using stdlib ServeMux's
// "METHOD /path" pattern syntax. If method is empty, the handler matches
// any verb (stdlib's bare-pattern behavior). This is the generic
// counterpart to Get/Post/Put/Patch/Delete, for verbs without a dedicated
// method (e.g. OPTIONS) or when the verb is only known at runtime. mw
// wraps h only, not the whole App — see Use for whole-app middleware.
func (a *App) Handle(method, pattern string, h http.Handler, mw ...Middleware) {
	if len(mw) > 0 {
		h = Chain(mw...)(h)
	}
	routed := pattern
	if method != "" {
		routed = method + " " + pattern
	}
	a.mux.Handle(routed, h)
}
