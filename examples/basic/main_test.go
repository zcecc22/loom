package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	gwt "git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git"
)

// Example demonstrates the App built by newApp (the same construction
// main() runs via Run) by exercising it directly through ServeHTTP,
// without blocking on an actual listener. It's a smoke test across the
// route set, not exhaustive — the library's own test suite covers each
// construct's edge cases in depth.
func Example() {
	logger := gwt.NewLogger("dev")
	app := newApp(gwt.Config{Env: "dev"}, logger)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Body.String())

	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/items/42", nil))
	fmt.Println(rec2.Code, rec2.Body.String())

	// /admin without credentials: rejected by Auth.
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/admin", nil))
	fmt.Println(rec3.Code)

	// /admin with credentials: accepted, Identity() round-trips.
	rec4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req4.SetBasicAuth("admin", "changeme")
	app.ServeHTTP(rec4, req4)
	fmt.Println(rec4.Code, rec4.Body.String())

	// ServeStatic.
	rec5 := httptest.NewRecorder()
	app.ServeHTTP(rec5, httptest.NewRequest(http.MethodGet, "/static/style.css", nil))
	fmt.Println(rec5.Code)

	// RenderTemplate.
	rec6 := httptest.NewRecorder()
	app.ServeHTTP(rec6, httptest.NewRequest(http.MethodGet, "/page", nil))
	fmt.Println(rec6.Code)

	// A panicking handler: caught by the automatic Recover middleware.
	rec7 := httptest.NewRecorder()
	app.ServeHTTP(rec7, httptest.NewRequest(http.MethodGet, "/panic", nil))
	fmt.Println(rec7.Code)

	// Output:
	// 200 {"hello":"world"}
	// 200 {"id":"42"}
	// 401
	// 200 {"logged_in_as":"admin"}
	// 200
	// 200
	// 500
}
