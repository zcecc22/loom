package gwt

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
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

// Option configures an App during construction. Option is always applied by
// New and never fails — anything that can meaningfully fail (like
// malformed env-based config) belongs in LoadConfig, called explicitly
// before New. Do not change this signature to return an error; it is
// deliberate (see CLAUDE.md's Conventions).
type Option func(*App)

// New builds an App by applying opts over sane defaults: a Config resolved
// the same way LoadConfig resolves its defaults, and a logger built from
// NewLogger(cfg.Env) if WithLogger was not supplied. New never fails or
// validates; invalid configuration (e.g. a negative timeout) surfaces
// later, at Run/RunContext.
//
// New unconditionally wires Logging and Recover as the App's global
// middleware — every App is logged and panic-safe with zero further setup.
// There is no option to opt out of this; use Use to add more middleware on
// top (stacked closer to the handler, so a panic or early exit inside it is
// still caught and logged).
func New(opts ...Option) *App {
	a := &App{
		mux: http.NewServeMux(),
		cfg: defaultConfig(),
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.logger == nil {
		a.logger = NewLogger(a.cfg.Env)
	}
	a.global = []Middleware{a.Logging(), a.Recover()}
	return a
}

// WithAddr sets the address the App listens on (e.g. ":8000").
func WithAddr(addr string) Option {
	return func(a *App) { a.cfg.Addr = addr }
}

// WithEnv sets the App's environment name (e.g. "dev", "prod"), which
// determines the default logger format (see NewLogger).
func WithEnv(env string) Option {
	return func(a *App) { a.cfg.Env = env }
}

// WithLogger sets the App's logger, overriding the default built from
// NewLogger(cfg.Env).
func WithLogger(l *slog.Logger) Option {
	return func(a *App) { a.logger = l }
}

// WithConfig applies cfg wholesale, typically the result of LoadConfig.
// Options are applied in order, so options listed after WithConfig
// override the individual fields they touch.
func WithConfig(cfg Config) Option {
	return func(a *App) { a.cfg = cfg }
}

// WithReadTimeout overrides Config.ReadTimeout.
func WithReadTimeout(d time.Duration) Option {
	return func(a *App) { a.cfg.ReadTimeout = d }
}

// WithReadHeaderTimeout overrides Config.ReadHeaderTimeout.
func WithReadHeaderTimeout(d time.Duration) Option {
	return func(a *App) { a.cfg.ReadHeaderTimeout = d }
}

// WithWriteTimeout overrides Config.WriteTimeout.
func WithWriteTimeout(d time.Duration) Option {
	return func(a *App) { a.cfg.WriteTimeout = d }
}

// WithIdleTimeout overrides Config.IdleTimeout.
func WithIdleTimeout(d time.Duration) Option {
	return func(a *App) { a.cfg.IdleTimeout = d }
}

// WithShutdownTimeout overrides Config.ShutdownTimeout, the grace period
// RunContext gives in-flight requests to complete during shutdown.
func WithShutdownTimeout(d time.Duration) Option {
	return func(a *App) { a.cfg.ShutdownTimeout = d }
}

// WithOnStart registers a hook run, in registration order, before the
// server starts serving. Any error (including a recovered panic) aborts
// startup before ListenAndServe is called; hooks already run are not
// unwound.
func WithOnStart(fn func(context.Context) error) Option {
	return func(a *App) { a.onStart = append(a.onStart, fn) }
}

// WithOnShutdown registers a hook run during shutdown, in LIFO order (last
// registered, first run), symmetric with defer. Every registered hook runs
// regardless of earlier hooks' failures.
func WithOnShutdown(fn func(context.Context) error) Option {
	return func(a *App) { a.onShutdown = append(a.onShutdown, fn) }
}

// WithBaseContext sets the function used to derive the base context for
// incoming connections, passed through to http.Server.BaseContext. This is
// deliberately independent from the context RunContext uses to trigger
// shutdown — conflating the two is a correctness bug (see CLAUDE.md's
// "Things to avoid").
func WithBaseContext(fn func(net.Listener) context.Context) Option {
	return func(a *App) { a.baseContextFn = fn }
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
// WithLogger or NewLogger(cfg.Env). It exists so callers that don't hold
// their own logger variable (see CLAUDE.md's minimal-boilerplate example)
// still have a way to log errors returned by Run/RunContext.
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
