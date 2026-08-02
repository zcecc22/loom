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
//	app.Get("/", func(w http.ResponseWriter, r *http.Request) {
//		app.RenderJSON(w, r, map[string]string{"hello": "world"}, http.StatusOK)
//	})
//	if err := app.Run(); err != nil {
//		app.Logger().Error("server exited", "err", err)
//	}
package gwt
