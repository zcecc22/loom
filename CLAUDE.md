# CLAUDE.md — gwt v2 (Go Web Toolkit)

Guide for Claude Code sessions working on this repo. Keep this short; read it first, then act.

> **Status:** All 22 Tier 1–5 backlog items are implemented and passing, plus three post-completion API revisions (embedded logger/default middleware/`Group` removed; functional options replaced by `AppConfig`; dead indirection removed — see ISSUES.md's "API revision" sections) and a Tier 6 adding static file serving, security middleware, authentication, and a scaffold update demonstrating all three in `gwt-cli new`'s generated app, all as of 2026-08-03. `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` are all clean. The library (`gwt`), the scaffolder (`cmd/gwt-cli`), and the reference example (`examples/basic`) all exist. See `ISSUES.md`'s "Recently completed" section for what shipped and its test coverage, and "Deferred" for what's intentionally still out of scope (form validation, session storage, CSRF).

## What this project is

`gwt` (module `git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git`, package `gwt`) is a **zero-dependency, stdlib-only Go web toolkit** — a redesign ("v2") of an earlier project of the same name (`github.com/zcecc22/gwt`, mirrored at `/home/zcecc22/git/gwt.git`). It is a thin helper layer over `net/http` that eliminates boilerplate around routing, middleware, server lifecycle, config, and JSON/HTML rendering. It is deliberately **not a framework**: no router DSL beyond what stdlib already gives us, no ORM, no DI container.

v2's scope, relative to v1:
- **Kept and hardened:** routing/middleware, JSON/HTML-template rendering, a project scaffolder with Docker deployment output.
- **New:** graceful server shutdown, plain-struct construction (`New(cfg ...AppConfig) *App`), startup/shutdown lifecycle hooks, fail-fast config loading, static file serving with directory-listing disabled, opt-in security-headers/body-size-limit/HSTS middleware, and a generic pluggable `Auth` middleware — none of which v1 had.
- **Rescoped API:** construction takes a plain `AppConfig` struct instead of v1's all-free-function style; middleware stays plain `func(http.Handler) http.Handler` composition. (v2 briefly used a functional-options constructor — `Option`/`WithX` — before switching to `AppConfig`; see ISSUES.md's second "API revision" section for why.)
- **Deferred (not built this phase, see ISSUES.md):** form parsing/validation, session storage/issuance, CSRF protection. (Static serving, security headers, and a generic auth *primitive* are now built — see Tier 6 — but session-store-backed auth and CSRF specifically remain deferred; `Auth`'s pluggable `authenticate` function is a building block someone could use to implement session-based auth on top, not a session implementation itself.)

## Design principles (non-negotiable without user approval)

1. **Stdlib only.** Zero external dependencies. Any change touching `go.mod` requires explicit user approval. This is the defining property carried over from v1 — routing rides on Go 1.22+ `http.ServeMux` method + wildcard patterns (`GET /items/{id}`) rather than a hand-rolled router, because stdlib already does it.
2. **Plain-struct construction; plain-function middleware.** `App` is built via `New(cfg ...AppConfig) *App`, where `AppConfig` is an ordinary exported struct — set the fields you want, leave the rest zero. `cfg` is variadic only so `gwt.New()` (no argument) still works for the zero-config case; passing more than one `AppConfig` is meaningless and only the first is used. There is no functional-options layer: no `Option` type, no `WithX` constructors, no closures involved in configuring an `App`. (v2 originally used functional options here — `type Option func(*App)` plus a dozen `WithX` functions — but a source-review pass found that indirection genuinely harder to follow than the plain struct it now is; see ISSUES.md's second "API revision" section.) Middleware is `type Middleware func(http.Handler) http.Handler` composed by wrapping — no fluent chainable builder, no heavyweight combinator/pipeline abstraction.
3. **The logger is embedded in `App`, and that's a deliberate, narrow exception to "explicit dependencies."** Every handler used to need `logger` threaded through every render call, and every app needed the same logger passed twice more to build its middleware stack — pure repetition with zero payoff, since there is exactly one logger per `App`. `RenderJSON`/`RenderTemplate`/`RenderServerError`/`Logging`/`Recover` are now `App` methods that read `a.logger` implicitly instead of taking it as a parameter. **This does not generalize** — it does not mean dependencies should become implicit generally. There is no service locator, no other hidden state, and no plan to embed anything else this way. If a future case seems to call for it, treat it as a new decision, not a precedent already set.
4. **Every `App` is logged and panic-safe with zero setup.** `New()` unconditionally wires `Logging()` and `Recover()` as the App's outermost middleware — there is no option to skip this. `gwt.New()` alone is a complete, safe starting point; `Use(mw ...Middleware)` adds anything further on top (stacked closer to the handler, so `Recover` still catches panics inside it). This is a deliberate behavior change, not just an ergonomics one — weigh it before ever making it optional.
5. **Idiomatic Go.** `net/http` handlers, `log/slog`, `html/template`. No reinventing stdlib patterns.
6. **The scaffold is the spec.** `gwt-cli new` must always produce a runnable, `go vet`-clean project with a working Docker deployment artifact — it is the canonical usage example, same role it played in v1. `examples/basic/main.go` is a secondary reference — a comprehensive single-file tour of every construct in the API, kept deliberately more exhaustive than the scaffold's own generated app — but it is still not shaped for scaffold-template reuse (the scaffolder has its own embedded templates under `cmd/gwt-cli/templates/`).
7. **No speculative abstractions.** New helpers land when a real use case needs them, not before. This is why `RequestID` middleware, route introspection (`App.Routes()`), and a `Logging`/`Recover` opt-out were cut to ISSUES.md's Deferred tier rather than built speculatively. It's also why `Group` (a prefix-scoped nested route builder) was built, then **removed** — routing is flat by design now: explicit verb methods plus one generic `Handle(method, pattern, h, mw...)`, nothing more.

## Repo layout

```
.
├── doc.go                 Package doc comment: what gwt is/isn't, minimal usage example
├── app.go                 App struct, AppConfig struct, New(), Use(), Logger()
├── router.go               Get/Post/Put/Patch/Delete/Handle — flat routing, no Group
├── middleware.go           Middleware type, Chain(), App.Logging(), App.Recover()
├── server.go               Run()/RunContext(), graceful shutdown, startup/shutdown hooks
├── config.go               Config struct, LoadConfig() (Config, error)
├── logging.go               NewLogger(env) *slog.Logger
├── render.go                App.RenderJSON/RenderTemplate/RenderServerError (+ unexported cores)
├── static.go                App.ServeStatic() — directory serving, listing disabled by default
├── security.go               App.SecureHeaders()/MaxBodySize()/HSTS() — all opt-in
├── auth.go                   App.Auth() + package-level Identity() — generic pluggable auth
├── *_test.go                 Co-located tests for each file above
├── examples/
│   └── basic/              Single-file tour of every construct (main.go + templates/ + static/)
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
- `type AppConfig struct { Config Config; Logger *slog.Logger; OnStart, OnShutdown []func(context.Context) error; BaseContext func(net.Listener) context.Context }` — every field optional, zero value = default. Set fields directly on a struct literal; there's no builder and no functional options involved.
- `New(cfg ...AppConfig) *App` — resolves `cfg[0]` if given (nothing if not), fills any zero-value `Config` field via `resolveConfig` (see `config.go`), defaults the logger via `NewLogger(cfg.Env)` if `AppConfig.Logger` wasn't set, then unconditionally sets the App's global middleware to `[]Middleware{a.Logging(), a.Recover()}`. `gwt.New()` alone is a complete, working, logged, panic-safe app. **`cfg` is variadic only to keep the zero-argument call working** — passing more than one `AppConfig` is meaningless and only `cfg[0]` is used; do not read anything into that variadic beyond "optional single struct."
- `App.Use(mw ...Middleware)` — the **only** mechanism for adding global (whole-app) middleware beyond the always-on `Logging`/`Recover` pair. Called any time after `New`, typically once, right after construction. Appends — stacked *after* the defaults, i.e. closer to the handler, so a panic or early exit inside `mw` is still caught/logged.
- `App.Logger() *slog.Logger` — returns the App's resolved logger. Exists so callers that don't hold their own logger variable (the common case now) still have a way to log `Run`/`RunContext`'s returned error.
- `App.Handler() http.Handler` — the fully-built handler (global middleware wrapped around the mux), for callers who want to run their own `http.Server`/supervisor instead of `Run`

Construction examples (three independent ways to call `New`, not meant to run together):

```go
app := gwt.New() // every default

app := gwt.New(gwt.AppConfig{Config: gwt.Config{Addr: ":9000"}})

cfg, _ := gwt.LoadConfig()
app := gwt.New(gwt.AppConfig{Config: cfg, Logger: myLogger})
```

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
- `AppConfig.BaseContext func(net.Listener) context.Context` — passthrough to `http.Server.BaseContext`, a deliberately separate mechanism from shutdown signaling.

**`config.go`**
- `type Config struct { Addr, Env string; ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout, ShutdownTimeout time.Duration }`
- `LoadConfig() (Config, error)` — reads `ADDR` (default `:8000`), `ENV` (default `dev`), `READ_TIMEOUT` (5s), `READ_HEADER_TIMEOUT` (2s), `WRITE_TIMEOUT` (10s), `IDLE_TIMEOUT` (120s), `SHUTDOWN_TIMEOUT` (10s). Fails fast on a malformed `*_TIMEOUT` value, naming the offending variable — a deliberate v1→v2 signature break (v1's `LoadConfig() Config` silently ignored bad input; v2 does not).
- `resolveConfig(c Config) Config` (unexported) — used by `New` to fill any zero-value field of `AppConfig.Config` with `defaultConfig()`'s value for that field. This is what makes `AppConfig{Config: Config{Addr: ":9000"}}` leave every other field (the timeouts especially) at a safe non-zero default instead of silently becoming an unset zero-value timeout. One consequence: there's no way to explicitly force a zero timeout through `AppConfig` — a zero value always reads as "unset, use the default." That trade-off is deliberate.

**`logging.go`**
- `NewLogger(env string) *slog.Logger` — `env == "dev"` → `slog.NewTextHandler(os.Stderr, ...)`; anything else → `slog.NewJSONHandler(os.Stderr, ...)`.

**`render.go`**
- `App.RenderJSON(w, r, data any, statusCode int)` — marshal + write using `a.logger`; logs on marshal error. Direct port of v1's `RenderJson`, now a method.
- `App.RenderTemplate(w, r, tmplPath string, data any, statusCode int)` — parses `tmplPath` per call, renders via `html/template` (auto-escaping, XSS-safe) into a `bytes.Buffer` before writing status + body, so partial writes on execute failure are impossible. Direct port of v1's `RenderTemplate`, including its buffered-write hardening, now a method.
- `App.RenderServerError(w, r, err error)` — log + `http.Error` 500 using `a.logger`; shared error path for both helpers above and for `Recover`.
- These are methods only — there is no free-function form and no unexported "core" layer underneath them either (an earlier version had one, delegating to `logger`-taking free functions, on the theory that `Recover` needed a form callable without an `App`; that was never true — `Recover` is itself an `App` method — so the indirection was removed once noticed). Don't add either form back without discussing it.

**`static.go`**
- `App.ServeStatic(urlPrefix, dir string, mw ...Middleware)` — registers `GET <urlPrefix>` (a subtree pattern; the caller supplies the trailing `/`, e.g. `"/static/"` — no implicit pattern rewriting, consistent with `Group`'s removal and the scaffolder's "no magic path transformations" principle) serving files from `dir` via `http.FileServer`. Wrapped in an unexported `noListingFileSystem` so a directory request without an `index.html` 404s instead of rendering a directory listing — the one behavior change from a bare `http.FileServer`. Path-traversal confinement is inherited from `http.Dir.Open`, not re-implemented.

**`security.go`** — all three are opt-in via `Use` or route-level `mw`, never automatic (unlike `Logging`/`Recover`): a header like `X-Frame-Options` can break a legitimate use case (intentional iframe embedding), so it isn't safe to force on every `App` by default.
- `App.SecureHeaders() Middleware` — sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`. Deliberately omits `X-XSS-Protection` (deprecated — the legacy filter it enabled had vulnerabilities of its own) and `Content-Security-Policy`/`Permissions-Policy` (no safe one-size-fits-all default; a wrong CSP breaks pages more than it helps, so it's left to the caller).
- `App.MaxBodySize(n int64) Middleware` — wraps `r.Body` in `http.MaxBytesReader(w, r.Body, n)`. Defense-in-depth against a large-body DoS regardless of whether a reverse proxy in front of the app already enforces a limit — don't assume it does.
- `App.HSTS() Middleware` — sets `Strict-Transport-Security: max-age=31536000; includeSubDomains`. Kept separate from `SecureHeaders` deliberately: it's only correct when this deployment is actually reached over HTTPS (directly or via a TLS-terminating reverse proxy) — enabling it for a plain-HTTP deployment (e.g. local dev) would be wrong, not just unnecessary. Enable it deliberately per-deployment (e.g. gated on `Env == "prod"`), don't bundle it into a default stack.

**`auth.go`**
- `App.Auth(authenticate func(r *http.Request) (identity string, ok bool)) Middleware` — calls `authenticate` per request; `!ok` → `401` (plain `http.Error`, no `WWW-Authenticate` header, since `Auth` doesn't know which scheme `authenticate` implements) and `next` is never called; `ok` → stashes `identity` in the request context and calls `next`. `authenticate` is where the real credential check lives (Basic via `r.BasicAuth()`, a bearer token, a signed cookie, whatever) — `Auth` itself is scheme-agnostic on purpose, so it doesn't lock callers into one auth scheme.
- `Identity(r *http.Request) (identity string, ok bool)` — package-level function (not a method — it reads from the request context, which doesn't need an `App`) that reads back what `Auth` stashed. `ok` is `false` for a request that never passed through `Auth`.
- `Auth` is **route-level** middleware — passed as a trailing `mw` argument to `Get`/`Post`/etc. — not global. Protecting a subset of routes is just a matter of only passing it to those routes' registration calls; no grouping mechanism is needed or provided (see `Group`'s removal).

**`cmd/gwt-cli`**
- `gwt-cli new <name>` — flags `-module <path>`, `-port <num>`, `-force`. Emits a runnable gwt app (`main.go`, `go.mod`) demonstrating every Tier 6 feature alongside the render helpers: a `RenderJSON` route (`/`), a `RenderTemplate` route (`/page`, `templates/index.tmpl`, linking the generated stylesheet), `ServeStatic` (`/static/`, `static/style.css`), `Use(app.SecureHeaders())` applied globally, and an `Auth`-protected `/admin` route reading demo credentials from `ADMIN_USER`/`ADMIN_PASSWORD` (app-specific env vars, read directly via `os.Getenv` in `main.go` — not part of `gwt.Config`/`LoadConfig`). Plus the Docker deployment set: `Dockerfile` (now copies `static/` into the runtime image alongside `templates/`), `docker-compose.yml`, `update_image`, `.env.example` (includes the two demo `ADMIN_*` vars), `.dockerignore`. `.in` files run through `text/template`; other files are copied verbatim. `go mod tidy` is left as a manual step so the scaffold does not depend on network access.

## Conventions

- **`New` is non-fallible by design.** `New(cfg ...AppConfig) *App` never returns an error and never validates — mistakes (negative timeout, empty addr) surface at `Run()` time, which already returns `error`. The one thing that can meaningfully fail — malformed env values — fails inside `LoadConfig() (Config, error)`, called explicitly by the caller before `New`. Don't add validation to `New` or change its signature without discussing it first; it was specified this way deliberately.
- **Global middleware wraps the whole built handler once**, at `Handler()`/`Run()` build time — not per-route. This means the App's global middleware (`Logging`/`Recover`, plus anything added via `Use`) also covers the mux's own 404/405 responses; route middleware (passed as trailing `mw ...Middleware` args to `Get`/`Post`/etc.) does not see those, since it only wraps the specific handler it's attached to. This is intentional, not an inconsistency.
- **Middleware order is fixed: `Logging` → `Recover` → anything added via `Use` (in the order `Use` was called) → route-level `mw` → handler.** `Logging` is outermost so it logs the true final status even when `Recover` converts a panic to a 500; `Recover` is next so it catches panics from `Use`-added middleware too, not just the handler.
- **`Logging` and `Recover` are the only *automatic* middleware.** Everything else added since — `SecureHeaders`, `MaxBodySize`, `HSTS`, `Auth` — is opt-in, via `Use` (whole-app) or route-level `mw` (per-route, `Auth`'s normal case). None of them get wired into `New` by default. Keep it that way: a header or an auth check can break a legitimate use case in a way logging/panic-recovery never do, so "safe for every App unconditionally" is a high, deliberately narrow bar that only `Logging`/`Recover` clear.
- **Shutdown hooks run LIFO** (`AppConfig.OnShutdown` is a slice in registration order, execution is last-registered-first), symmetric with Go's own `defer` semantics. Startup hooks (`AppConfig.OnStart`) run FIFO, in registration order, and any error aborts startup before `ListenAndServe` is ever called. If a startup hook fails partway through a list, hooks already run are **not** automatically unwound — no rollback machinery, deliberately (stdlib-only, no transaction abstraction).
- **The logger is the one exception to "handlers receive dependencies explicitly."** It's embedded in `App` and read implicitly by `App`'s own methods (`RenderJSON`, `RenderTemplate`, `RenderServerError`, `Logging`, `Recover`). Everything else stays explicit: no package-level state, no globals, no service locator. See Design Principle 3 for why this one exception exists and why it isn't a precedent for more.
- Verb-first naming: `RenderJSON`, not `JSONRender`. File-per-role: routing in `router.go`, middleware in `middleware.go`, etc. — create a new file only if a genuinely new category appears.
- Every exported symbol must have a godoc comment. `go vet ./...` must stay clean.
- slog keys are lowercase, no punctuation (`addr`, `method`, `uri`, `env`). Never `"Method:"` or similar.

## Testing

Stdlib only (`testing`, `net/http/httptest`, `encoding/json`). Co-locate tests: `middleware_test.go` next to `middleware.go`, etc. Every public function should have at least a happy-path and an error-path case. `go test -race ./...` must stay clean, particularly around `Run`'s goroutine and shutdown signaling — repeated start/stop cycles under `-race` are part of the suite, not optional.

## Things to avoid

- **Do not** add external dependencies (router, template engine, ORM, validation lib, session lib, assertion lib). If you think one is needed, propose it in ISSUES.md and wait for approval.
- **Do not** wire `RunContext`'s shutdown-trigger context as `http.Server.BaseContext`. This is a correctness bug, not a style preference: doing so cancels every in-flight request's context the instant the shutdown signal fires, silently defeating graceful drain — the server still looks like it "shut down gracefully" from the outside, but every in-flight handler sees a canceled context and likely fails. `AppConfig.BaseContext` is the only knob for `BaseContext`, and it defaults to stdlib's own per-connection `context.Background()` if unset. The shutdown context passed to `server.Shutdown()` must be derived fresh (`context.WithTimeout(context.Background(), cfg.ShutdownTimeout)`), independent of both the trigger context (already canceled) and `BaseContext`.
- **Do not** add form validation, session storage/issuance, or CSRF protection "while in there." These are explicitly deferred — see ISSUES.md's Deferred tier. (Static file serving, security headers, and the generic `Auth` primitive are explicitly **in** scope now, not deferred — don't treat them as excluded. `Auth` is a building block, not a session implementation — don't extend it into session storage without a fresh, explicit request.)
- **Do not** add an opt-out for the default `Logging`+`Recover` wrapping, and **do not** reintroduce `Group` or any other prefix-scoped/nested route builder, without explicit user approval. Both were deliberate decisions (not oversights) reached after the initial implementation shipped — see ISSUES.md's "API revision" section for the reasoning.
- **Do not** widen the "logger is implicit" exception to other dependencies. If a handler needs something else `App`-embedded (a DB pool, a config value), pass it explicitly — don't add it to `App` and read it implicitly the way the logger is, unless the user asks for that specific change.
- **Do not** reintroduce a functional-options constructor (`type Option func(*App)`, `WithX` functions) without explicit user approval. It was tried, found harder to read on review than a plain struct, and deliberately replaced by `AppConfig` — see ISSUES.md's second "API revision" section.
- **Do not** add features "while you're in there." Scope your changes to the task.

## Where to look next

- **ISSUES.md** — the backlog, tier-ordered (foundation → rendering → scaffolding/Docker → quality → framework capabilities → static/security/auth). All items through Tier 6 are checked off under "Recently completed"; the active backlog is empty and everything else is intentionally "Deferred," with reasons. Always check it before proposing new work; the user may have already decided the direction.
