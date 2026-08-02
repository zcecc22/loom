# CLAUDE.md — gwt v2 (Go Web Toolkit)

Guide for Claude Code sessions working on this repo. Keep this short; read it first, then act.

> **Status:** All 22 Tier 1–5 backlog items are implemented and passing, plus a post-completion API simplification (embedded logger, default middleware, `Group` removed — see ISSUES.md's "API revision" section) as of 2026-08-02. `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` are all clean. The library (`gwt`), the scaffolder (`cmd/gwt-cli`), and the reference example (`examples/basic`) all exist. See `ISSUES.md`'s "Recently completed" section for what shipped and its test coverage, and "Deferred" for what's intentionally still out of scope (static file serving, form validation, sessions/auth/CSRF).

## What this project is

`gwt` (module `git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git`, package `gwt`) is a **zero-dependency, stdlib-only Go web toolkit** — a redesign ("v2") of an earlier project of the same name (`github.com/zcecc22/gwt`, mirrored at `/home/zcecc22/git/gwt.git`). It is a thin helper layer over `net/http` that eliminates boilerplate around routing, middleware, server lifecycle, config, and JSON/HTML rendering. It is deliberately **not a framework**: no router DSL beyond what stdlib already gives us, no ORM, no DI container.

v2's scope, relative to v1:
- **Kept and hardened:** routing/middleware, JSON/HTML-template rendering, a project scaffolder with Docker deployment output.
- **New:** graceful server shutdown, functional-options construction, startup/shutdown lifecycle hooks, fail-fast config loading.
- **Rescoped API:** functional options for construction (`New(opts ...Option) *App`) instead of v1's all-free-function style; middleware stays plain `func(http.Handler) http.Handler` composition.
- **Deferred (not built this phase, see ISSUES.md):** static file serving, form parsing/validation, sessions/auth/CSRF.

## Design principles (non-negotiable without user approval)

1. **Stdlib only.** Zero external dependencies. Any change touching `go.mod` requires explicit user approval. This is the defining property carried over from v1 — routing rides on Go 1.22+ `http.ServeMux` method + wildcard patterns (`GET /items/{id}`) rather than a hand-rolled router, because stdlib already does it.
2. **Functional options for construction; plain-function middleware.** `App` is built via `New(opts ...Option) *App` where `Option` is `func(*App)`. Middleware is `type Middleware func(http.Handler) http.Handler` composed by wrapping — no fluent chainable builder, no heavyweight combinator/pipeline abstraction. This is v2's defining API shift from v1's all-free-function style, chosen because options scale to optional config without exploding constructor arity, while middleware stays exactly as idiomatic-Go as it already was.
3. **The logger is embedded in `App`, and that's a deliberate, narrow exception to "explicit dependencies."** Every handler used to need `logger` threaded through every render call, and every app needed the same logger passed twice more to build its middleware stack — pure repetition with zero payoff, since there is exactly one logger per `App`. `RenderJSON`/`RenderTemplate`/`RenderServerError`/`Logging`/`Recover` are now `App` methods that read `a.logger` implicitly instead of taking it as a parameter. **This does not generalize** — it does not mean dependencies should become implicit generally. There is no service locator, no other hidden state, and no plan to embed anything else this way. If a future case seems to call for it, treat it as a new decision, not a precedent already set.
4. **Every `App` is logged and panic-safe with zero setup.** `New()` unconditionally wires `Logging()` and `Recover()` as the App's outermost middleware — there is no option to skip this. `gwt.New()` alone is a complete, safe starting point; `Use(mw ...Middleware)` adds anything further on top (stacked closer to the handler, so `Recover` still catches panics inside it). This is a deliberate behavior change, not just an ergonomics one — weigh it before ever making it optional.
5. **Idiomatic Go.** `net/http` handlers, `log/slog`, `html/template`. No reinventing stdlib patterns.
6. **The scaffold is the spec.** `gwt-cli new` must always produce a runnable, `go vet`-clean project with a working Docker deployment artifact — it is the canonical usage example, same role it played in v1. `examples/basic/main.go` is a secondary, simpler reference; it is not shaped for scaffold-template reuse (the scaffolder has its own embedded templates under `cmd/gwt-cli/templates/`).
7. **No speculative abstractions.** New helpers land when a real use case needs them, not before. This is why `RequestID` middleware, route introspection (`App.Routes()`), and a `Logging`/`Recover` opt-out were cut to ISSUES.md's Deferred tier rather than built speculatively. It's also why `Group` (a prefix-scoped nested route builder) was built, then **removed** — routing is flat by design now: explicit verb methods plus one generic `Handle(method, pattern, h, mw...)`, nothing more.

## Repo layout

```
.
├── doc.go                 Package doc comment: what gwt is/isn't, minimal usage example
├── app.go                 App struct, Option type, New(), core options, Use(), Logger()
├── router.go               Get/Post/Put/Patch/Delete/Handle — flat routing, no Group
├── middleware.go           Middleware type, Chain(), App.Logging(), App.Recover()
├── server.go               Run()/RunContext(), graceful shutdown, startup/shutdown hooks
├── config.go               Config struct, LoadConfig() (Config, error)
├── logging.go               NewLogger(env) *slog.Logger
├── render.go                App.RenderJSON/RenderTemplate/RenderServerError (+ unexported cores)
├── *_test.go                 Co-located tests for each file above
├── examples/
│   └── basic/main.go       Minimal runnable reference program (secondary to the scaffold)
├── cmd/
│   └── gwt-cli/            `gwt-cli new <name>` project scaffolder
│       ├── main.go            Subcommand dispatcher + flags (-module, -port, -force)
│       ├── scaffold.go        Embedded-FS manifest + template engine
│       └── templates/         //go:embed all:templates — app source + Docker deploy files
├── go.mod                  Go 1.24.4, zero external deps
├── CLAUDE.md                This file
└── ISSUES.md                 Prioritized backlog — read before proposing new work
```

**Note on the package/import-path mismatch:** the module path ends in `go-web-toolkit.git`, but the package clause is `package gwt` (brand continuity with v1). This is legal Go and `goimports`/`go doc` resolve it correctly, but don't be surprised by it — a naive guess from the import path alone would land on `go-web-toolkit` or worse.

## Public API surface

All symbols live in package `gwt` at the module root, except the scaffolder CLI under `cmd/gwt-cli`.

**`app.go`**
- `type App struct { ... }` — holds the mux, global middleware, config, logger, lifecycle hooks. Implements `http.Handler` (`ServeHTTP`), so it drops straight into `httptest.NewServer(app)` or nests inside another handler.
- `type Option func(*App)` — functional option. **Signature is fixed** — do not change it to return an error; `New` never fails or validates (see Conventions below).
- `New(opts ...Option) *App` — applies options over defaults (zero-value `Config` resolved the same way `LoadConfig` resolves it; default logger from `NewLogger(cfg.Env)` if `WithLogger` wasn't supplied), then unconditionally sets the App's global middleware to `[]Middleware{a.Logging(), a.Recover()}`. `gwt.New()` alone is a complete, working, logged, panic-safe app.
- `WithAddr(addr string) Option`, `WithEnv(env string) Option`, `WithLogger(l *slog.Logger) Option`
- `WithReadTimeout`, `WithReadHeaderTimeout`, `WithWriteTimeout`, `WithIdleTimeout`, `WithShutdownTimeout(d time.Duration) Option` — granular timeout overrides
- `WithConfig(cfg Config) Option` — apply a whole `Config` (typically `LoadConfig()`'s result) in one call; later options in the list win, since options apply in order
- `WithOnStart(fn func(context.Context) error) Option` — appended, run in registration order before serving begins
- `WithOnShutdown(fn func(context.Context) error) Option` — appended, run in **LIFO** order during shutdown (see Conventions)
- `App.Use(mw ...Middleware)` — the **only** mechanism for adding global (whole-app) middleware beyond the always-on `Logging`/`Recover` pair. A plain method (not an `Option`), called any time after `New`, typically once, right after construction. Appends — stacked *after* the defaults, i.e. closer to the handler, so a panic or early exit inside `mw` is still caught/logged.
- `App.Logger() *slog.Logger` — returns the App's resolved logger. Exists so callers that don't hold their own logger variable (the common case now) still have a way to log `Run`/`RunContext`'s returned error.
- `App.Handler() http.Handler` — the fully-built handler (global middleware wrapped around the mux), for callers who want to run their own `http.Server`/supervisor instead of `Run`

**`router.go`** — flat routing only, no nesting/prefixing.
- `App.Get/Post/Put/Patch/Delete(pattern string, h http.HandlerFunc, mw ...Middleware)` — thin sugar over stdlib `ServeMux` method+wildcard patterns; path params read via stdlib `r.PathValue("id")` directly (not wrapped — wrapping a one-line stdlib call would itself be a speculative abstraction).
- `App.Handle(method, pattern string, h http.Handler, mw ...Middleware)` — the generic counterpart to the explicit verb methods: full `http.Handler`, verb passed as a plain string argument, `method == ""` matches any verb. Use this for verbs without a dedicated method (e.g. `OPTIONS`) or when the verb is only known at runtime.

**`middleware.go`**
- `type Middleware func(http.Handler) http.Handler`
- `Chain(mw ...Middleware) Middleware` — `Chain(A, B, C)(h) == A(B(C(h)))`; declared order equals request-flow order (A's pre-handler code runs first).
- `App.Logging() Middleware` — logs method/path/status/duration once per request, using `a.logger`. v1's `RequestLogger` retyped as a method.
- `App.Recover() Middleware` — recovers panics, renders a 500 via `a.RenderServerError`, using `a.logger`. v1's `RecoverPanic` retyped as a method. `New` wires `Chain(a.Logging(), a.Recover())` automatically — Logging outermost so it still logs the final status even when Recover converts a panic to a 500 (direct port of v1's `RequestLogger(RecoverPanic(...))` wiring, now automatic instead of hand-assembled).

**`server.go`**
- `App.Run() error` — convenience: derives its context from `signal.NotifyContext(os.Interrupt, syscall.SIGTERM)`.
- `App.RunContext(ctx context.Context) error` — `ctx` is used for exactly one thing: knowing *when* to begin shutdown. See Things to avoid for the `BaseContext` distinction.
- `WithBaseContext(fn func(net.Listener) context.Context) Option` — passthrough to `http.Server.BaseContext`, a deliberately separate mechanism from shutdown signaling.

**`config.go`**
- `type Config struct { Addr, Env string; ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout, ShutdownTimeout time.Duration }`
- `LoadConfig() (Config, error)` — reads `ADDR` (default `:8000`), `ENV` (default `dev`), `READ_TIMEOUT` (5s), `READ_HEADER_TIMEOUT` (2s), `WRITE_TIMEOUT` (10s), `IDLE_TIMEOUT` (120s), `SHUTDOWN_TIMEOUT` (10s). Fails fast on a malformed `*_TIMEOUT` value, naming the offending variable — a deliberate v1→v2 signature break (v1's `LoadConfig() Config` silently ignored bad input; v2 does not).

**`logging.go`**
- `NewLogger(env string) *slog.Logger` — `env == "dev"` → `slog.NewTextHandler(os.Stderr, ...)`; anything else → `slog.NewJSONHandler(os.Stderr, ...)`.

**`render.go`**
- `App.RenderJSON(w, r, data any, statusCode int)` — marshal + write using `a.logger`; logs on marshal error. Direct port of v1's `RenderJson`, now a method.
- `App.RenderTemplate(w, r, tmplPath string, data any, statusCode int)` — parses `tmplPath` per call, renders via `html/template` (auto-escaping, XSS-safe) into a `bytes.Buffer` before writing status + body, so partial writes on execute failure are impossible. Direct port of v1's `RenderTemplate`, including its buffered-write hardening, now a method.
- `App.RenderServerError(w, r, err error)` — log + `http.Error` 500 using `a.logger`; shared error path for both helpers above and for `Recover`.
- These are methods only — there is no free-function form. Each delegates to an unexported implementation (`renderJSON`/`renderTemplate`/`renderServerError`, taking `logger` explicitly) so `Recover` and the other render methods can share the same code without an `App` receiver where one isn't available. Don't add the free-function form back without discussing it — it was deliberately removed.

**`cmd/gwt-cli`**
- `gwt-cli new <name>` — flags `-module <path>`, `-port <num>`, `-force`. Emits a runnable gwt app (`main.go`, `go.mod`, a demo `RenderJSON` route, a demo `RenderTemplate` route with `templates/index.tmpl`) plus the Docker deployment set: `Dockerfile`, `docker-compose.yml`, `update_image`, `.env.example`, `.dockerignore`. `.in` files run through `text/template`; other files are copied verbatim. `go mod tidy` is left as a manual step so the scaffold does not depend on network access.

## Conventions

- **`Option` is non-fallible by design.** `New(opts ...Option) *App` never returns an error and never validates — mistakes (negative timeout, empty addr) surface at `Run()` time, which already returns `error`. The one thing that can meaningfully fail — malformed env values — fails inside `LoadConfig() (Config, error)`, called explicitly by the caller before `New`. Don't add validation to `New` or change `Option`'s signature without discussing it first; it was specified this way deliberately.
- **Global middleware wraps the whole built handler once**, at `Handler()`/`Run()` build time — not per-route. This means the App's global middleware (`Logging`/`Recover`, plus anything added via `Use`) also covers the mux's own 404/405 responses; route middleware (passed as trailing `mw ...Middleware` args to `Get`/`Post`/etc.) does not see those, since it only wraps the specific handler it's attached to. This is intentional, not an inconsistency.
- **Middleware order is fixed: `Logging` → `Recover` → anything added via `Use` (in the order `Use` was called) → route-level `mw` → handler.** `Logging` is outermost so it logs the true final status even when `Recover` converts a panic to a 500; `Recover` is next so it catches panics from `Use`-added middleware too, not just the handler.
- **Shutdown hooks run LIFO** (`WithOnShutdown` appends, execution is last-registered-first), symmetric with Go's own `defer` semantics. Startup hooks (`WithOnStart`) run FIFO, in registration order, and any error aborts startup before `ListenAndServe` is ever called. If a startup hook fails partway through a list, hooks already run are **not** automatically unwound — no rollback machinery, deliberately (stdlib-only, no transaction abstraction).
- **The logger is the one exception to "handlers receive dependencies explicitly."** It's embedded in `App` and read implicitly by `App`'s own methods (`RenderJSON`, `RenderTemplate`, `RenderServerError`, `Logging`, `Recover`). Everything else stays explicit: no package-level state, no globals, no service locator. See Design Principle 3 for why this one exception exists and why it isn't a precedent for more.
- Verb-first naming: `RenderJSON`, not `JSONRender`. File-per-role: routing in `router.go`, middleware in `middleware.go`, etc. — create a new file only if a genuinely new category appears.
- Every exported symbol must have a godoc comment. `go vet ./...` must stay clean.
- slog keys are lowercase, no punctuation (`addr`, `method`, `uri`, `env`). Never `"Method:"` or similar.

## Testing

Stdlib only (`testing`, `net/http/httptest`, `encoding/json`). Co-locate tests: `middleware_test.go` next to `middleware.go`, etc. Every public function should have at least a happy-path and an error-path case. `go test -race ./...` must stay clean, particularly around `Run`'s goroutine and shutdown signaling — repeated start/stop cycles under `-race` are part of the suite, not optional.

## Things to avoid

- **Do not** add external dependencies (router, template engine, ORM, validation lib, session lib, assertion lib). If you think one is needed, propose it in ISSUES.md and wait for approval.
- **Do not** wire `RunContext`'s shutdown-trigger context as `http.Server.BaseContext`. This is a correctness bug, not a style preference: doing so cancels every in-flight request's context the instant the shutdown signal fires, silently defeating graceful drain — the server still looks like it "shut down gracefully" from the outside, but every in-flight handler sees a canceled context and likely fails. `WithBaseContext` is the only knob for `BaseContext`, and it defaults to stdlib's own per-connection `context.Background()` if unset. The shutdown context passed to `server.Shutdown()` must be derived fresh (`context.WithTimeout(context.Background(), cfg.ShutdownTimeout)`), independent of both the trigger context (already canceled) and `BaseContext`.
- **Do not** add static file serving, form validation, or auth/session/CSRF "while in there." These are explicitly deferred — see ISSUES.md's Deferred tier. (The scaffolder and JSON/template rendering are explicitly **in** scope, not deferred — don't treat them as excluded.)
- **Do not** add an opt-out for the default `Logging`+`Recover` wrapping, and **do not** reintroduce `Group` or any other prefix-scoped/nested route builder, without explicit user approval. Both were deliberate decisions (not oversights) reached after the initial implementation shipped — see ISSUES.md's "API revision" section for the reasoning.
- **Do not** widen the "logger is implicit" exception to other dependencies. If a handler needs something else `App`-embedded (a DB pool, a config value), pass it explicitly — don't add it to `App` and read it implicitly the way the logger is, unless the user asks for that specific change.
- **Do not** add features "while you're in there." Scope your changes to the task.

## Where to look next

- **ISSUES.md** — the backlog, tier-ordered (foundation → rendering → scaffolding/Docker → quality → framework capabilities). All 22 items are currently checked off under "Recently completed"; the active backlog is empty and everything else is intentionally "Deferred," with reasons. Always check it before proposing new work; the user may have already decided the direction.
