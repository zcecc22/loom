// Package gwt is a zero-dependency, stdlib-only toolkit that eliminates
// boilerplate around routing, middleware, server lifecycle, configuration,
// and JSON/HTML-template rendering for net/http applications. It is
// deliberately not a framework: there is no router DSL beyond what
// net/http's ServeMux already provides, no ORM, no dependency-injection
// container.
//
// A minimal application — New alone is logged and panic-safe, no further
// setup required:
//
//	app := gwt.New()
//	app.GET("/", func(c *gwt.Ctx) error {
//		return c.JSON(200, gwt.H{"hello": "world"})
//	})
//	if err := app.Run(); err != nil {
//		app.Logger().Error("server exited", "err", err)
//	}
//
// # Handlers
//
// Handlers take a *Ctx and return an error. Ctx wraps the request and
// response with helpers — Param, Query, Bind on the way in; JSON, String,
// HTML, NoContent, Redirect on the way out — and its W and R fields stay
// exported, so dropping down to plain net/http is always available.
//
// Returning an error is what keeps failure paths to one line. Status and
// StatusErr build an *HTTPError carrying a status code and a client-safe
// message; any other error becomes a 500 whose details are logged but
// never sent:
//
//	app.GET("/items/{id}", func(c *gwt.Ctx) error {
//		item, err := store.Find(c.Param("id"))
//		if err != nil {
//			return gwt.Status(404, "no such item")
//		}
//		return c.JSON(200, item)
//	})
//
// # Middleware
//
// Middleware is plain net/http — func(http.Handler) http.Handler — so
// anything written for the stdlib composes with gwt directly. Pass it to
// Use for the whole app, or as trailing arguments to a route for just that
// route. Logging and Recover are always on; SecureHeaders, MaxBodySize,
// HSTS, and Auth are opt-in.
package gwt
