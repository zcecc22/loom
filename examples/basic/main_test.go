package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	gwt "git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git"
)

// Example demonstrates the App built by newApp (the same construction
// main() runs via Run) by exercising it directly through ServeHTTP,
// without blocking on an actual listener. It's a smoke test across the
// route set, not exhaustive — the library's own test suite covers each
// construct's edge cases in depth.
func Example() {
	logger := gwt.NewLogger("dev")
	app, err := newApp(gwt.Config{Env: "dev"}, logger)
	if err != nil {
		fmt.Println("setup failed:", err)
		return
	}

	// get issues a request and prints the status, plus the body when the
	// route returns one worth showing.
	get := func(req *http.Request, showBody bool) {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if showBody {
			fmt.Println(rec.Code, rec.Body.String())
			return
		}
		fmt.Println(rec.Code)
	}

	get(httptest.NewRequest(http.MethodGet, "/", nil), true)
	get(httptest.NewRequest(http.MethodGet, "/items/42?format=short", nil), true)
	get(httptest.NewRequest(http.MethodGet, "/hello/world", nil), true)

	// Bind rejects a malformed body with a 400, not a 500.
	get(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader("{oops")), true)
	// A valid body that fails the handler's own check.
	get(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"name":""}`)), true)
	get(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"name":"widget"}`)), true)

	// Status supplies the client-visible message; StatusErr keeps the
	// wrapped cause in the log and sends only the standard text.
	get(httptest.NewRequest(http.MethodGet, "/missing", nil), true)
	get(httptest.NewRequest(http.MethodGet, "/missing-wrapped", nil), true)
	// A plain error: 500, with the real cause logged rather than sent.
	get(httptest.NewRequest(http.MethodGet, "/boom", nil), true)

	// /admin without credentials: rejected by Auth.
	get(httptest.NewRequest(http.MethodGet, "/admin", nil), false)

	// /admin with credentials: accepted, Identity() round-trips.
	authed := httptest.NewRequest(http.MethodGet, "/admin", nil)
	authed.SetBasicAuth("admin", "changeme")
	get(authed, true)

	// ServeStatic, Ctx.HTML, and the automatic Recover middleware.
	get(httptest.NewRequest(http.MethodGet, "/static/style.css", nil), false)
	get(httptest.NewRequest(http.MethodGet, "/page", nil), false)
	get(httptest.NewRequest(http.MethodGet, "/panic", nil), false)

	// Output:
	// 200 {"hello":"world"}
	// 200 {"format":"short","id":"42"}
	// 200 hello, world
	// 400 {"error":"invalid JSON body"}
	// 400 {"error":"name is required"}
	// 201 {"name":"widget"}
	// 404 {"error":"no such item"}
	// 404 {"error":"Not Found"}
	// 500 {"error":"Internal Server Error"}
	// 401
	// 200 {"logged_in_as":"admin"}
	// 200
	// 200
	// 500
}
