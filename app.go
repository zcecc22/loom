package gwt

import (
	"context"
	"log/slog"
	"net"
	"net/http"
)

// App is a gwt application: a stdlib http.ServeMux plus global middleware,
// configuration, a logger, and lifecycle hooks. App implements http.Handler,
// so it can be passed directly to httptest.NewServer or nested inside
// another handler.
type App struct {
	mux    *http.ServeMux
	global []Middleware
	cfg    Config
	logger *slog.Logger

	onStart    []func(context.Context) error
	onShutdown []func(context.Context) error

	baseContextFn func(net.Listener) context.Context
}

// AppConfig configures an App at construction. Every field is optional —
// its zero value produces the same default New always used. There's no
// builder and no functional options here: set the fields you care about
// on a plain struct literal.
type AppConfig struct {
	// Config holds the HTTP server's address, environment, and timeouts.
	// A zero-value field within it falls back to the same default
	// LoadConfig would use — set only the fields you want to override.
	// This is typically LoadConfig()'s result, passed straight through.
	Config Config

	// Logger overrides the default logger built from NewLogger(Config.Env).
	Logger *slog.Logger

	// OnStart hooks run, in registration order, before the server starts
	// serving. The first error aborts startup; hooks already run are not
	// unwound.
	OnStart []func(context.Context) error

	// OnShutdown hooks run in LIFO order during graceful shutdown (last
	// registered, first run — symmetric with defer). Every hook runs
	// regardless of earlier hooks' failures.
	OnShutdown []func(context.Context) error

	// BaseContext, if set, is passed through to http.Server.BaseContext.
	// Deliberately independent from the context RunContext uses to
	// trigger shutdown — conflating the two is a correctness bug, see
	// CLAUDE.md's "Things to avoid".
	BaseContext func(net.Listener) context.Context
}

// New builds an App from cfg. cfg is optional: New() with no argument is
// a complete, working App — logged, panic-safe, listening on the default
// address, with zero further setup. Passing more than one AppConfig is
// meaningless; only cfg[0] is used.
//
// New never fails or validates; a mistake in cfg (e.g. a nonsensical
// BaseContext) surfaces later, at Run/RunContext.
//
// New unconditionally wires Logging and Recover as the App's global
// middleware — there is no way to construct an App without them. Use Use
// to add more middleware on top (stacked closer to the handler, so a
// panic or early exit inside it is still caught and logged).
func New(cfg ...AppConfig) *App {
	var c AppConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}

	a := &App{
		mux:           http.NewServeMux(),
		cfg:           resolveConfig(c.Config),
		logger:        c.Logger,
		onStart:       c.OnStart,
		onShutdown:    c.OnShutdown,
		baseContextFn: c.BaseContext,
	}
	if a.logger == nil {
		a.logger = NewLogger(a.cfg.Env)
	}
	a.global = []Middleware{a.Logging(), a.Recover()}
	return a
}

// Use appends mw to the App's global middleware, stacked after the
// always-on Logging and Recover pair New installs (i.e. closer to the
// handler — a panic or early exit inside mw is still caught and logged).
// Use is the only mechanism for adding further whole-app middleware; it
// can be called any time after New, typically once, right after
// construction.
func (a *App) Use(mw ...Middleware) {
	a.global = append(a.global, mw...)
}

// Logger returns the App's logger — the one resolved by New from
// AppConfig.Logger or NewLogger(cfg.Env). It exists so callers that don't
// hold their own logger variable still have a way to log errors returned
// by Run/RunContext.
func (a *App) Logger() *slog.Logger {
	return a.logger
}

// Handler returns the fully-built http.Handler: the App's mux wrapped by
// its global middleware (Logging and Recover, plus anything added via
// Use), applied once. Use this to run the App under a custom http.Server
// or supervisor instead of Run/RunContext.
func (a *App) Handler() http.Handler {
	var h http.Handler = a.mux
	if len(a.global) > 0 {
		h = Chain(a.global...)(h)
	}
	return h
}

// ServeHTTP implements http.Handler by delegating to Handler().
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.Handler().ServeHTTP(w, r)
}
