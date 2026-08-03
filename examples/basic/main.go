// Command basic is a single-file tour of every construct gwt provides:
// config loading (LoadConfig, NewLogger), full construction (AppConfig —
// Config, Logger, OnStart, OnShutdown, BaseContext), all five routing
// verbs plus the generic Handle, middleware (Use, route-level mw, Chain,
// the automatic Logging/Recover pair, and the opt-in
// SecureHeaders/MaxBodySize/HSTS), rendering (RenderJSON, RenderTemplate,
// RenderServerError), static file serving (ServeStatic), authentication
// (Auth, Identity), and both startup patterns (Run, and Handler embedded
// in your own *http.Server or mux).
//
// See cmd/gwt-cli for the canonical scaffolded-project template with
// Docker deployment output; this file is a broader single-file API tour
// and is not shaped for scaffold-template reuse.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"

	gwt "git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git"
)

// Item is the demo payload used by the /items routes.
type Item struct {
	Name string `json:"name"`
}

func newApp(cfg gwt.Config, logger *slog.Logger) *gwt.App {
	app := gwt.New(gwt.AppConfig{
		Config: cfg,
		Logger: logger,
		OnStart: []func(context.Context) error{
			func(ctx context.Context) error {
				logger.Info("startup hook: warming up")
				return nil
			},
		},
		OnShutdown: []func(context.Context) error{
			func(ctx context.Context) error {
				logger.Info("shutdown hook: cleaning up")
				return nil
			},
		},
		// BaseContext is deliberately independent from the context Run
		// uses to trigger shutdown — conflating the two is a correctness
		// bug, see CLAUDE.md's "Things to avoid". nil (the zero value)
		// would already default to this; set explicitly here just to
		// show the field.
		BaseContext: func(net.Listener) context.Context {
			return context.Background()
		},
	})

	// Logging and Recover are wired automatically by New — no code needed
	// for those two. Everything else is opt-in: SecureHeaders and
	// MaxBodySize apply to every route via Use; HSTS is deliberately left
	// out unless this is actually HTTPS-fronted (e.g. env == "prod").
	app.Use(app.SecureHeaders(), app.MaxBodySize(1<<20)) // cap bodies at 1 MiB
	if cfg.Env == "prod" {
		app.Use(app.HSTS())
	}

	// --- Rendering: RenderJSON, RenderTemplate, RenderServerError ---

	app.Get("/", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]string{"hello": "world"}, http.StatusOK)
	})

	app.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]string{"id": r.PathValue("id")}, http.StatusOK)
	})

	app.Post("/items", func(w http.ResponseWriter, r *http.Request) {
		var in Item
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			app.RenderServerError(w, r, err)
			return
		}
		app.RenderJSON(w, r, in, http.StatusCreated)
	})

	app.Put("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]string{"id": r.PathValue("id"), "op": "replaced"}, http.StatusOK)
	})

	app.Patch("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]string{"id": r.PathValue("id"), "op": "updated"}, http.StatusOK)
	})

	app.Delete("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Handle is the generic verb registration: any method as a plain
	// string, used here for a verb without its own dedicated method.
	app.Handle(http.MethodOptions, "/items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	}))

	app.Get("/page", func(w http.ResponseWriter, r *http.Request) {
		app.RenderTemplate(w, r, "templates/index.tmpl", map[string]string{"Name": "kitchen sink"}, http.StatusOK)
	})

	app.Get("/boom", func(w http.ResponseWriter, r *http.Request) {
		app.RenderServerError(w, r, errors.New("simulated failure"))
	})

	app.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("simulated panic — caught by the automatic Recover middleware")
	})

	// --- Static files ---

	app.ServeStatic("/static/", "static")

	// --- Route-level middleware and Chain ---

	tagHeader := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Example", "tagged")
			next.ServeHTTP(w, r)
		})
	}
	timingHeader := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Example-Timed", "1")
			next.ServeHTTP(w, r)
		})
	}
	// Chain combines multiple gwt.Middleware values into one, for when a
	// route needs more than one but they belong together conceptually.
	app.Get("/chained", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]bool{"ok": true}, http.StatusOK)
	}, gwt.Chain(tagHeader, timingHeader))

	// --- Authentication ---

	adminUser := envOrDefault("ADMIN_USER", "admin")
	adminPassword := envOrDefault("ADMIN_PASSWORD", "changeme")
	app.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		user, _ := gwt.Identity(r)
		app.RenderJSON(w, r, map[string]string{"logged_in_as": user}, http.StatusOK)
	}, app.Auth(func(r *http.Request) (string, bool) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != adminUser || pass != adminPassword {
			return "", false
		}
		return user, true
	}))

	return app
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	cfg, err := gwt.LoadConfig()
	if err != nil {
		slog.Error("gwt: invalid config", "err", err)
		os.Exit(1)
	}

	logger := gwt.NewLogger(cfg.Env)
	app := newApp(cfg, logger)

	if os.Getenv("CUSTOM_SERVER") != "" {
		// App.Handler() returns the fully-built handler (global
		// middleware + routes), for when you want to embed gwt as one
		// piece of a larger stdlib net/http setup instead of letting
		// Run/RunContext own the whole *http.Server. Here it's mounted
		// under a plain http.ServeMux alongside a non-gwt route.
		mux := http.NewServeMux()
		mux.Handle("/", app.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		logger.Info("gwt: starting custom server", "addr", cfg.Addr)
		if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
			logger.Error("server exited with error", "err", err)
			os.Exit(1)
		}
		return
	}

	// Run derives its shutdown-trigger context from OS signals and calls
	// RunContext internally — call RunContext directly instead if you
	// need your own cancellation source (e.g. a larger app's own
	// shutdown signal).
	if err := app.Run(); err != nil {
		app.Logger().Error("server exited with error", "err", err)
		os.Exit(1)
	}
}
