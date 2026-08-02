// Command basic is a minimal runnable gwt application demonstrating that
// New requires zero setup — a logged, panic-safe App is ready immediately.
// See cmd/gwt-cli for the canonical scaffolded-project template with
// Docker deployment output; this example is a simpler secondary reference
// and is not shaped for scaffold-template reuse.
package main

import (
	"net/http"
	"os"

	gwt "git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git"
)

func newApp() *gwt.App {
	app := gwt.New()

	app.Get("/", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]string{"hello": "world"}, http.StatusOK)
	})

	app.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]string{"id": r.PathValue("id")}, http.StatusOK)
	})

	return app
}

func main() {
	app := newApp()
	if err := app.Run(); err != nil {
		app.Logger().Error("server exited with error", "err", err)
		os.Exit(1)
	}
}
