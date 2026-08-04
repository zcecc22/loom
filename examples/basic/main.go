// Command basic is a single-file tour of every construct gwt provides:
// config loading (LoadServerConfig, NewLogger), full construction (AppConfig —
// Server, Logger, Templates, ErrorHandler, OnStart, OnShutdown,
// BaseContext), all five routing verbs plus the generic Handle and the
// net/http escape hatch HandleHTTP, middleware (Use, route-level mw,
// Chain, the automatic Logging/Recover pair, and the opt-in
// SecureHeaders/MaxBodySize/HSTS), the Ctx request/response helpers
// (Param, Query, Bind, JSON, String, HTML, NoContent, Redirect), error
// handling (returning an error, Ctx.Error, Ctx.ErrorErr, HTTPError), template
// loading (LoadTemplates), static file serving (ServeStatic),
// authentication (Authentication, Ctx.Identity), and both startup patterns (Run,
// and Handler embedded in your own *http.Server or mux).
//
// See cmd/gwt-cli for the canonical scaffolded-project template with
// Docker deployment output; this file is a broader single-file API tour
// and is not shaped for scaffold-template reuse.
package main

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"

	gwt "github.com/zcecc22/go-web-toolkit"
)

// templatesFS embeds the template set, so the binary renders correctly no
// matter what directory it runs from. LoadTemplates takes any fs.FS, so
// os.DirFS("templates") would work here too — at the cost of needing the
// files alongside the binary at runtime.
//
//go:embed templates
var templatesFS embed.FS

// Item is the demo payload used by the /items routes.
type Item struct {
	Name string `json:"name"`
}

// errNoSuchItem stands in for whatever a real storage layer would return.
var errNoSuchItem = errors.New("no such item")

func newApp(cfg gwt.ServerConfig, logger *slog.Logger) (*gwt.App, error) {
	// cfg is passed in explicitly here (built from LoadServerConfig in
	// main, below) so the logger built from cfg.Env exists before the App
	// does. New would already load the same environment on its own if cfg
	// were left zero — passing it through here just makes that resolved
	// value and this function's logger agree.
	app, err := gwt.New(gwt.AppConfig{
		Server: cfg,
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
	if err != nil {
		return nil, err
	}

	// Parsed once, here, rather than per request. Templates could equally
	// have been passed as AppConfig.Templates above.
	if err := app.LoadTemplates(templatesFS, "templates/*.tmpl"); err != nil {
		return nil, err
	}

	// Logging and Recover are wired automatically by New — no code needed
	// for those two. Everything else is opt-in: SecureHeaders and
	// MaxBodySize apply to every route via Use; HSTS is deliberately left
	// out unless this is actually HTTPS-fronted (e.g. env == "prod").
	app.Use(app.SecureHeaders(), app.MaxBodySize(1<<20)) // cap bodies at 1 MiB
	if cfg.Env == "prod" {
		app.Use(app.HSTS())
	}

	// --- Responses: Ctx.JSON, Ctx.HTML, Ctx.String, Ctx.NoContent ---

	app.GET("/", func(c *gwt.Ctx) error {
		return c.JSON(http.StatusOK, gwt.H{"hello": "world"})
	})

	// Param reads a stdlib ServeMux wildcard; Query reads the URL query.
	app.GET("/items/{id}", func(c *gwt.Ctx) error {
		return c.JSON(http.StatusOK, gwt.H{
			"id":     c.Param("id"),
			"format": c.QueryDefault("format", "full"),
		})
	})

	// Bind decodes JSON and, on malformed input, returns an error that
	// already carries a 400 — so the whole failure path is `return err`.
	app.POST("/items", func(c *gwt.Ctx) error {
		var in Item
		if err := c.Bind(&in); err != nil {
			return err
		}
		if in.Name == "" {
			return c.Error(http.StatusBadRequest, "name is required")
		}
		return c.JSON(http.StatusCreated, in)
	})

	app.PUT("/items/{id}", func(c *gwt.Ctx) error {
		return c.JSON(http.StatusOK, gwt.H{"id": c.Param("id"), "op": "replaced"})
	})

	app.PATCH("/items/{id}", func(c *gwt.Ctx) error {
		return c.JSON(http.StatusOK, gwt.H{"id": c.Param("id"), "op": "updated"})
	})

	app.DELETE("/items/{id}", func(c *gwt.Ctx) error {
		return c.NoContent(http.StatusNoContent)
	})

	// Handle is the generic verb registration: any method as a plain
	// string, used here for a verb without its own dedicated method.
	app.Handle(http.MethodOptions, "/items/{id}", func(c *gwt.Ctx) error {
		c.SetHeader("Allow", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		return c.NoContent(http.StatusNoContent)
	})

	// HTML renders a named template from the set loaded above.
	app.GET("/page", func(c *gwt.Ctx) error {
		return c.HTML(http.StatusOK, "index.tmpl", gwt.H{"Name": "kitchen sink"})
	})

	// String writes text literally; Stringf formats (and go vet checks the
	// format string, because of the f suffix).
	app.GET("/hello/{name}", func(c *gwt.Ctx) error {
		return c.Stringf(http.StatusOK, "hello, %s", c.Param("name"))
	})

	app.GET("/old-page", func(c *gwt.Ctx) error {
		return c.Redirect(http.StatusFound, "/page")
	})

	// --- Errors ---

	// Error sets the status and a client-visible message.
	app.GET("/missing", func(c *gwt.Ctx) error {
		return c.Error(http.StatusNotFound, "no such item")
	})

	// ErrorErr wraps an internal cause: the client sees the standard text
	// for the status, the cause goes to the log only.
	app.GET("/missing-wrapped", func(c *gwt.Ctx) error {
		return c.ErrorErr(http.StatusNotFound, errNoSuchItem)
	})

	// A plain error is a 500 whose detail never reaches the client.
	app.GET("/boom", func(c *gwt.Ctx) error {
		return errors.New("simulated failure")
	})

	app.GET("/panic", func(c *gwt.Ctx) error {
		panic("simulated panic — caught by the automatic Recover middleware")
	})

	// --- Static files ---

	// ServeStatic takes an fs.FS, so both os.DirFS (here — read from disk
	// at the working directory) and an embedded //go:embed static work.
	// The scaffold (cmd/gwt-cli) demonstrates the embedded form.
	app.ServeStatic("/static/", os.DirFS("static"))

	// HandleHTTP mounts a plain net/http handler, bypassing Ctx — the
	// escape hatch for third-party handlers, proxies, and the like.
	app.HandleHTTP(http.MethodGet, "/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

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
	// Middleware stays plain net/http, so anything already written against
	// func(http.Handler) http.Handler drops straight in. Chain combines
	// several into one.
	app.GET("/chained", func(c *gwt.Ctx) error {
		return c.JSON(http.StatusOK, gwt.H{"ok": true})
	}, gwt.Chain(tagHeader, timingHeader))

	// --- Authentication ---

	adminUser := envOrDefault("ADMIN_USER", "admin")
	adminPassword := envOrDefault("ADMIN_PASSWORD", "changeme")
	app.GET("/admin", func(c *gwt.Ctx) error {
		user, _ := c.Identity()
		return c.JSON(http.StatusOK, gwt.H{"logged_in_as": user})
	}, app.Authentication(func(r *http.Request) (string, bool) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != adminUser || pass != adminPassword {
			return "", false
		}
		return user, true
	}))

	return app, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	cfg, err := gwt.LoadServerConfig()
	if err != nil {
		slog.Error("gwt: invalid config", "err", err)
		os.Exit(1)
	}

	logger := gwt.NewLogger(cfg.Env)
	app, err := newApp(cfg, logger)
	if err != nil {
		logger.Error("gwt: setup failed", "err", err)
		os.Exit(1)
	}

	if os.Getenv("CUSTOM_SERVER") != "" {
		// App.Handler() returns the fully-built handler (global
		// middleware + routes), for when you want to embed gwt as one
		// piece of a larger stdlib net/http setup instead of letting
		// Run/RunContext own the whole *http.Server. Here it's mounted
		// under a plain http.ServeMux alongside a non-gwt route.
		mux := http.NewServeMux()
		mux.Handle("/", app.Handler())
		mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
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
	// shutdown signal). Run(":9000") would override ServerConfig.Addr.
	if err := app.Run(); err != nil {
		app.Logger().Error("server exited with error", "err", err)
		os.Exit(1)
	}
}
