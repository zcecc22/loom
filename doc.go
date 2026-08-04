// Package gwt is a zero-dependency, stdlib-only toolkit that eliminates
// boilerplate around routing, middleware, server lifecycle, configuration,
// and JSON/HTML-template rendering for net/http applications. It is
// deliberately not a framework: there is no router DSL beyond what
// net/http's ServeMux already provides, no ORM, no dependency-injection
// container.
//
// A minimal application — New alone is logged and panic-safe, no further
// setup required. It also loads its server config from the environment
// itself (ADDR, ENV, the *_TIMEOUT variables), which is the only thing
// that can make it fail:
//
//	app, err := gwt.New()
//	if err != nil {
//		log.Fatal(err)
//	}
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
// Returning an error is what keeps failure paths to one line. Ctx.Error
// and Ctx.ErrorErr build an *HTTPError carrying a status code and a
// client-safe message; any other error becomes a 500 whose details are
// logged but never sent:
//
//	app.GET("/items/{id}", func(c *gwt.Ctx) error {
//		item, err := store.Find(c.Param("id"))
//		if err != nil {
//			return c.Error(404, "no such item")
//		}
//		return c.JSON(200, item)
//	})
//
// Every error gwt itself generates renders the same way — a JSON body of
// the form {"error":"<message>"} — whether it came from a handler, a
// panic, an Auth rejection, or the router finding no route. The two
// exceptions are handlers mounted with HandleHTTP and ServeStatic, which
// own their own responses; see HandleHTTP.
//
// # Middleware
//
// Middleware is plain net/http — func(http.Handler) http.Handler — so
// anything written for the stdlib composes with gwt directly. Pass it to
// Use for the whole app, or as trailing arguments to a route for just that
// route. Logging and Recover are always on; SecureHeaders, MaxBodySize,
// HSTS, and Auth are opt-in.
package gwt
