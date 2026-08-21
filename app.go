package loom

import (
	"context"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// App is a loom application: a stdlib http.ServeMux plus global middleware,
// configuration, a logger, a template set, and lifecycle hooks. App
// implements http.Handler, so it can be passed directly to
// httptest.NewServer or nested inside another handler.
type App struct {
	mux    *http.ServeMux
	global []Middleware
	cfg    serverConfig
	logger *slog.Logger

	templates    *template.Template
	errorHandler func(*Ctx, error)

	onStart    []func(context.Context) error
	onShutdown []func(context.Context) error

	baseContextFn func(net.Listener) context.Context

	// buildOnce guards built, so the global middleware chain is composed
	// once rather than per request.
	buildOnce sync.Once
	built     http.Handler
}

// AppConfig configures an App at construction. Every field is optional —
// its zero value produces the same default New always used. There's no
// builder and no functional options here: set the fields you care about
// on a plain struct literal.
type AppConfig struct {
	// Addr is the address the server listens on, e.g. ":8000". New already
	// loads this from the environment itself (see New); set it here only to
	// override what the environment provided (or the hardcoded default, if
	// the environment didn't set it either). Same precedence for every
	// field down through ShutdownTimeout below.
	Addr string
	// Env is the environment name (e.g. "dev", "prod"); it selects
	// NewLogger's default handler format.
	Env string
	// ReadTimeout is http.Server.ReadTimeout.
	ReadTimeout time.Duration
	// ReadHeaderTimeout is http.Server.ReadHeaderTimeout.
	ReadHeaderTimeout time.Duration
	// WriteTimeout is http.Server.WriteTimeout.
	WriteTimeout time.Duration
	// IdleTimeout is http.Server.IdleTimeout.
	IdleTimeout time.Duration
	// ShutdownTimeout is the grace period RunContext gives in-flight
	// requests to complete during a graceful shutdown.
	ShutdownTimeout time.Duration

	// Logger overrides the default logger built from NewLogger(cfg.Env).
	Logger *slog.Logger

	// Templates is the template set Ctx.HTML renders from. Setting it here
	// is equivalent to calling LoadTemplates, for callers who already have
	// a parsed set (e.g. one built with extra funcs via template.FuncMap).
	Templates *template.Template

	// ErrorHandler overrides how a non-nil error returned by a Handler is
	// turned into a response. The default logs the error and writes a JSON
	// body, using an *HTTPError's status and message when the chain has
	// one and a bare 500 otherwise — see HTTPError.
	//
	// A custom handler must check Ctx.Written before writing, since a
	// handler can respond in full and then still return an error.
	ErrorHandler func(*Ctx, error)

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
// New loads its server config from the environment itself, via
// LoadServerConfig — ADDR, ENV, and the *_TIMEOUT variables are picked up
// automatically, with no separate call needed. Precedence is: an explicit
// AppConfig field (Addr, Env, or one of the timeouts), then the
// environment, then the hardcoded default. This is the one thing that can
// make New panic: a malformed *_TIMEOUT environment variable, same as
// LoadServerConfig's own contract — a construction-time misconfiguration,
// not a per-request condition, so it fails loudly at setup rather than
// being swallowed. Anything else in cfg (e.g. a nonsensical BaseContext)
// is not validated here and surfaces later, at Run/RunContext.
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
		cfg:           resolveServerConfig(c, LoadServerConfig()).server(),
		logger:        c.Logger,
		templates:     c.Templates,
		errorHandler:  c.ErrorHandler,
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
// Use is the only mechanism for adding further whole-app middleware.
//
// Call Use before serving starts, typically once, right after New: the
// global chain is composed on the first call to Handler/ServeHTTP and
// cached, so a Use after that point has no effect.
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
//
// The chain is composed on the first call and cached, so later Use calls
// are not reflected — see Use.
func (a *App) Handler() http.Handler {
	a.buildOnce.Do(func() {
		// muxErrorsAsJSON sits innermost, directly around the mux, so the
		// global middleware still wraps — and still logs and recovers —
		// the mux's own 404/405 responses.
		var h http.Handler = a.muxErrorsAsJSON(a.mux)
		if len(a.global) > 0 {
			h = Chain(a.global...)(h)
		}
		a.built = h
	})
	return a.built
}

// ServeHTTP implements http.Handler by delegating to Handler().
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.Handler().ServeHTTP(w, r)
}
