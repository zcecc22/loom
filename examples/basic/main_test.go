package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

// Example demonstrates the App built by newApp (the same construction
// main() runs via Run) by exercising it directly through ServeHTTP,
// without blocking on an actual listener.
func Example() {
	app := newApp()

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(rec.Code, rec.Body.String())

	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/items/42", nil))
	fmt.Println(rec2.Code, rec2.Body.String())

	// Output:
	// 200 {"hello":"world"}
	// 200 {"id":"42"}
}
