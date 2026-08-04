# CLAUDE.md — gwt v3 (Go Web Toolkit)

Guide for Claude Code sessions working on this repo. Keep this short; read it first, then act.

> **Status:** All 22 Tier 1–5 backlog items plus Tier 6 (static/security/auth) are implemented and passing, followed by three v2-era API revisions (embedded logger/default middleware/`Group` removed; functional options replaced by `AppConfig`; dead indirection removed) and, as of 2026-08-03, **v3: a gin-style `Ctx` API** — handlers are now `func(*gwt.Ctx) error`, with response/request helpers on `Ctx`, an `*HTTPError` error contract, a loaded template set, and the `Render*` helpers removed. See ISSUES.md's "API revision — v3" section for the full reasoning and what was explicitly declined. The module has since moved to its public home at `github.com/zcecc22/go-web-toolkit` — see ISSUES.md's "publishing prep" section. `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` are all clean. The library (`gwt`), the scaffolder (`cmd/gwt-cli`), and the reference example (`examples/basic`) all exist. See `ISSUES.md`'s "Recently completed" for what shipped and its test coverage, and "Deferred" for what's intentionally still out of scope (form validation, session storage, CSRF).

## What this project is

`gwt` (module `github.com/zcecc22/go-web-toolkit`, package `gwt`) is a **zero-dependency, stdlib-only Go web toolkit** — a redesign of an earlier project of the same name (`github.com/zcecc22/gwt`, mirrored at `/home/zcecc22/git/gwt.git`). It is a thin helper layer over `net/http` that eliminates boilerplate around routing, middleware, server lifecycle, config, and JSON/HTML rendering. It is deliberately **not a framework**: no router DSL beyond what stdlib already gives us, no ORM, no DI container.

Scope, relative to v1:
- **Kept and hardened:** routing/middleware, JSON/HTML-template rendering, a project scaffolder with Docker deployment output.
- **New:** graceful server shutdown, plain-struct construction (`New(cfg ...AppConfig) *App`), startup/shutdown lifecycle hooks, fail-fast config loading, static file serving with directory-listing disabled, opt-in security-headers/body-size-limit/HSTS middleware, and a generic pluggable `Authentication` middleware — none of which v1 had.
- **v3's rescoped API:** handlers take a `*Ctx` and return an `error`; rendering moved from `App.Render*(w, r, ...)` free-standing calls to `Ctx` methods (`c.JSON`, `c.HTML`, `c.String`); templates are parsed once via `LoadTemplates` rather than per request. Construction still takes a plain `AppConfig` struct, and **middleware is still plain `func(http.Handler) http.Handler`** — see Design Principle 2 for why that one deliberately did *not* become `Ctx`-native.
- **Deferred (see ISSUES.md):** form parsing/validation, session storage/issuance, CSRF protection. (Static serving, security headers, and a generic auth *primitive* are built — but session-store-backed auth and CSRF specifically remain deferred; `Authentication`'s pluggable `authenticate` function is a building block someone could use to implement session-based auth on top, not a session implementation itself.)

## Design principles (non-negotiable without user approval)

1. **Stdlib only.** Zero external dependencies. Any change touching `go.mod` requires explicit user approval. This is the defining property carried over from v1 — routing rides on Go 1.22+ `http.ServeMux` method + wildcard patterns (`GET /items/{id}`) rather than a hand-rolled router, because stdlib already does it.
2. **Handlers are `Ctx`-based; middleware stays plain `net/http`.** `type Handler func(*Ctx) error` for route handlers, but `type Middleware func(http.Handler) http.Handler` — unchanged — for middleware. This asymmetry is deliberate and load-bearing: **global middleware must cover the mux's own 404/405 responses**, and an unmatched request never reaches a route, so it has no `Ctx` and no path wildcards. A `Ctx`-native middleware chain would have to synthesize a meaningless `Ctx` around the bare mux. Gin escapes this because its router owns `NoRoute`; stdlib `ServeMux` gives no such hook. Keeping the stdlib shape also means any third-party or stdlib `http.Handler` middleware drops straight in — a real asset for a zero-dependency toolkit. The `Ctx` is built in the route adapter inside `Handle`, which is the only place it is constructed.
3. **Plain-struct construction.** `App` is built via `New(cfg ...AppConfig) (*App, error)`, where `AppConfig` is an ordinary exported struct — set the fields you want, leave the rest zero. `cfg` is variadic only so `gwt.New()` (no argument) still works for the zero-config case; passing more than one `AppConfig` is meaningless and only the first is used. There is no functional-options layer: no `Option` type, no `WithX` constructors. (That was tried and found harder to follow on review than the plain struct it now is; see ISSUES.md's second "API revision" section.) **`New` also loads its `ServerConfig` from the environment itself** (via `LoadServerConfig`) — see Conventions for why this made `New` fallible, a deliberate 2026-08-04 reversal of the "New never fails" rule that stood since v2.
4. **The logger is embedded in `App`, and that's a deliberate, narrow exception to "explicit dependencies."** There is exactly one logger per `App`, and threading it through every render call was pure repetition. `Ctx.Logger()`, `Logging`, `Recover`, and the error handler all read `a.logger` implicitly. **This does not generalize** — no service locator, no other hidden state, no plan to embed anything else this way. If a future case seems to call for it, treat it as a new decision, not a precedent already set.
5. **Every `App` is logged and panic-safe with zero setup.** `New()` unconditionally wires `Logging()` and `Recover()` as the App's outermost middleware — there is no option to skip this. `gwt.New()` alone is a complete, safe starting point; `Use(mw ...Middleware)` adds anything further on top (stacked closer to the handler, so `Recover` still catches panics inside it). This is a deliberate behavior change, not just an ergonomics one — weigh it before ever making it optional.
6. **Idiomatic Go.** `net/http`, `log/slog`, `html/template`, `fs.FS`. No reinventing stdlib patterns. `Ctx.W` and `Ctx.R` stay exported so dropping to raw `net/http` is always one field away — `Ctx` is a convenience layer, never a wall.
7. **The scaffold is the spec.** `gwt-cli new` must always produce a runnable, `go vet`-clean project with a working Docker deployment artifact — it is the canonical usage example, same role it played in v1. `examples/basic/main.go` is a secondary reference — a comprehensive single-file tour of every construct in the API, kept deliberately more exhaustive than the scaffold's own generated app — but it is still not shaped for scaffold-template reuse (the scaffolder has its own embedded templates under `cmd/gwt-cli/templates/`).
8. **No speculative abstractions.** New helpers land when a real use case needs them, not before. This is why `RequestID` middleware, route introspection (`App.Routes()`), and a `Logging`/`Recover` opt-out were cut to ISSUES.md's Deferred tier rather than built speculatively. It's also why `Group` (a prefix-scoped nested route builder) was built, then **removed** — routing is flat by design: explicit verb methods plus one generic `Handle(method, pattern, h, mw...)`, nothing more. **Flat routing was reaffirmed during the v3 redesign**, when reintroducing `Group` was explicitly offered and declined. It is also why gin's `c.Set`/`c.Get` were **not** ported — see Conventions.

## Repo layout

```
.
├── doc.go                 Package doc: what gwt is/isn't, handler + middleware model
├── app.go                 App struct, AppConfig struct, New(), Use(), Logger(), Handler()
├── ctx.go                 Ctx, Handler, H — request/response helpers (the v3 core)
├── errors.go              HTTPError, Ctx.Error(), Ctx.ErrorErr(), App.handleError
├── router.go              GET/POST/PUT/PATCH/DELETE, Handle, HandleHTTP — flat routing
├── middleware.go          Middleware type, Chain(), App.Logging(), App.Recover(), statusWriter
├── server.go              Run()/RunContext(), graceful shutdown, startup/shutdown hooks
├── config.go              ServerConfig struct, LoadServerConfig() (ServerConfig, error)
├── logging.go             NewLogger(env) *slog.Logger
├── render.go              App.LoadTemplates() + unexported serverError (http-level 500 path)
├── static.go              App.ServeStatic() — fs.FS serving (disk or embedded), listing disabled by default
├── security.go            App.SecureHeaders()/MaxBodySize()/HSTS() — all opt-in
├── auth.go                App.Authentication() + package-level Identity() — generic pluggable auth
├── *_test.go              Co-located tests for each file above
├── examples/
│   └── basic/             Single-file tour of every construct (main.go + templates/ + static/)
├── cmd/
│   └── gwt-cli/           `gwt-cli new <name>` project scaffolder
│       ├── main.go            Subcommand dispatcher + flags (-module, -port, -force)
│       ├── scaffold.go        Embedded-FS manifest + template engine
│       └── templates/         //go:embed all:templates — app source + Docker deploy files
├── go.mod                 Go 1.26.5, zero external deps
├── README.md              User-facing docs — a step-by-step tutorial plus the full public API, with worked examples
├── CLAUDE.md              This file
└── ISSUES.md              Prioritized backlog — read before proposing new work
```

**Documentation is four files, and an API change touches all four:** `README.md` (user-facing), `doc.go` (godoc), `CLAUDE.md` (this file — design rationale and internal surface), and `ISSUES.md` (history and declined alternatives). `README.md` is the one most easily forgotten because nothing compiles it — but its snippets are the first thing a user copies, so verify them against the real library rather than by eye.

**Note on the package/import-path mismatch:** the module path is `github.com/zcecc22/go-web-toolkit`, but the package clause is `package gwt` (brand continuity with v1). This is legal Go and `goimports`/`go doc` resolve it correctly, but don't be surprised by it — a naive guess from the import path alone would land on `go-web-toolkit`, not `gwt`.

## The API in 8 lines

```go
app := gwt.New()
app.GET("/items/{id}", func(c *gwt.Ctx) error {
	item, err := store.Find(c.Param("id"))
	if err != nil {
		return gwt.Status(404, "no such item")
	}
	return c.JSON(200, item)
})
app.Run()
```

## Public API surface

All symbols live in package `gwt` at the module root, except the scaffolder CLI under `cmd/gwt-cli`.

**`ctx.go`** — the v3 core.
- `type H map[string]any` — JSON object shorthand: `gwt.H{"hello": "world"}`.
- `type Handler func(*Ctx) error` — a route handler. Returning non-nil hands off to the App's error handler; that's what keeps failure paths to one line.
- `type Ctx struct { W http.ResponseWriter; R *http.Request; ... }` — built once per matched route, in `Handle`'s adapter. `W`/`R` exported on purpose (Principle 6).
- Request: `Param(name)` (stdlib `PathValue`), `Query(name)`, `QueryDefault(name, def)`, `FormValue(name)`, `GetHeader(name)`, `Bind(v any) error` (JSON decode; a malformed body yields a **400**, not a 500), `Context()`, `Value(key any) any`, `Identity() (string, bool)`, `Logger()`.
- Response: `JSON(status, data) error`, `String(status, s) error`, `Stringf(status, format, args...) error`, `HTML(status, name, data) error`, `NoContent(status) error`, `Redirect(status, url) error`, `SetHeader(k, v)`, `Written() bool`.
- **`String` vs `Stringf`:** `String` writes its argument literally (a stray `%` is safe); `Stringf` formats. The `f` suffix is what lets `go vet` check the format string — a single variadic `String` made `go vet` flag every non-constant argument, which is why they're split.
- Every response helper funnels through one unexported `write`, which is the single place `wroteHeader` is set.

**`errors.go`**
- `type HTTPError struct { Code int; Message string; Err error }` with `Error()` and `Unwrap()`. `Message` is client-visible; `Err` is logged, **never sent**.
- `Ctx.Error(code int, message ...string) error` — message defaults to the standard text for `code`. A `Ctx` method (not a package-level function, unlike v3's original `Status`) so it's consistent with every other response-related helper (`c.JSON`, `c.HTML`, ...) — every real call site already has a `c` in scope, since it's only ever called from inside a `Handler`. Doesn't touch `c.W`/`c.R`/`c.app`, so a zero-value `&Ctx{}` works fine for unit-testing it directly.
- `Ctx.ErrorErr(code int, err error) error` — wraps a cause; client sees only the standard text.
- `App.handleError` (unexported) — uses `AppConfig.ErrorHandler` if set, else `defaultErrorHandler`: `errors.As` finds an `*HTTPError` **anywhere in the chain** and uses its status/message; anything else is a 500 with a generic body. 4xx logs at Warn, 5xx at Error (so a bad URL can't fill the error log).
- **The committed-response guard:** because `return c.JSON(...)` is the documented idiom, a handler can respond in full and *then* return an error (a write failure, a deferred cleanup). `handleError` checks `c.Written()` and only logs in that case — writing a second status would corrupt the response.

**`app.go`**
- `type App struct { ... }` — mux, global middleware, config, logger, template set, error handler, lifecycle hooks, and the cached built handler. Implements `http.Handler`.
- `type AppConfig struct { Server ServerConfig; Logger *slog.Logger; Templates *template.Template; ErrorHandler func(*Ctx, error); OnStart, OnShutdown []func(context.Context) error; BaseContext func(net.Listener) context.Context }` — every field optional, zero value = default.
- `New(cfg ...AppConfig) (*App, error)` — resolves `cfg[0]` if given, calls `LoadServerConfig()` to get an env-resolved `ServerConfig` (propagating its error, if any), fills any zero-value field of `cfg[0].Server` from that via `resolveServerConfig`, defaults the logger via `NewLogger(cfg.Env)`, then unconditionally sets global middleware to `[]Middleware{a.Logging(), a.Recover()}`. **`cfg` is variadic only to keep the zero-argument call working.** Precedence: explicit `AppConfig.Server` field > environment variable > hardcoded default — see Conventions.
- `App.Use(mw ...Middleware)` — the **only** mechanism for global middleware beyond the always-on pair. Appends, stacked *after* the defaults (closer to the handler).
- `App.Logger() *slog.Logger`, `App.Handler() http.Handler`.
- **`Handler()` composes the chain once, under a `sync.Once`, and caches it.** `ServeHTTP` used to rebuild the whole chain per request. Consequence to know: a `Use` call after serving has begun is **silently ignored**. Call `Use` before serving, right after `New`.

**`router.go`** — flat routing only, no nesting/prefixing.
- `App.GET/POST/PUT/PATCH/DELETE(pattern string, h Handler, mw ...Middleware)` — uppercase, gin-style. Five verbs, same as before. **No `HEAD` or `OPTIONS` method**: stdlib `ServeMux` already matches HEAD against a `GET` pattern, and `Handle`'s documented reason for existing is verbatim "for verbs without a dedicated method (e.g. `OPTIONS`)" — adding one would contradict the reason `Handle` survived `Group`'s deletion.
- `App.Handle(method, pattern string, h Handler, mw ...Middleware)` — generic verb as a plain string; `method == ""` matches any verb. This is where the `Ctx` adapter lives.
- `App.HandleHTTP(method, pattern string, h http.Handler, mw ...Middleware)` — the raw `net/http` escape hatch, bypassing `Ctx` entirely (third-party handlers, proxies, `http.FileServer`). `ServeStatic` and `Handle` both build on it. Its registered wrapper **steps out of `notFoundWriter`** before running the handler — see below.
- `muxErrorsAsJSON` / `notFoundWriter` (unexported) — makes the mux's own plain-text 404/405 come out in gwt's JSON error shape. `Handler` installs `muxErrorsAsJSON` as the innermost global wrapper.
  - **It works by elimination, not by asking the mux anything.** Every registered route steps out of `notFoundWriter` at the top of its `HandleHTTP` wrapper, so anything still writing through that wrapper is necessarily the mux answering for itself. Statuses below 400 pass through untouched, which is what keeps stdlib's trailing-slash and path-cleaning **3xx redirects** working (the specific code has changed across Go versions — 301 under Go 1.24, 307 as of Go 1.26 — the mechanism doesn't care which) — mistaking one for "no route" would turn every such redirect into a 404.
  - **Do not replace this with a `ServeMux.Handler(r)` pattern lookup.** It reads more directly, and it was tried: `ServeMux.Handler` costs ~200ns/48B/2allocs, *as much as the entire `mux.ServeHTTP` it precedes*, on every request — to benefit only responses that 404. It also cannot be used to serve: the handler it returns does not populate path wildcards, so `Ctx.Param` silently returns `""`. `TestPathValuesSurviveTheNotFoundWrapper` guards that.
  - Stepping out also keeps a route's writer the fully-featured `statusWriter`, so `notFoundWriter` never sits in a streaming or hijacking path and needs only `Unwrap`, not the full optional-interface forwarding.

**`middleware.go`**
- `type Middleware func(http.Handler) http.Handler` — **unchanged in v3, deliberately** (Principle 2).
- `Chain(mw ...Middleware) Middleware` — `Chain(A, B, C)(h) == A(B(C(h)))`; declared order equals request-flow order.
- `App.Logging() Middleware` — logs method/path/status/duration once per request.
- `App.Recover() Middleware` — recovers panics, logs with a stack, renders a 500 via the unexported `a.serverError` (it is http-level, so it has no `Ctx`).
- `statusWriter` wraps every response to record the status. It **forwards `Flush`, `Hijack`, and `ReadFrom`, and exposes `Unwrap() http.ResponseWriter`** for `http.ResponseController`. This matters because `Logging` is always on and unremovable: anything `statusWriter` fails to forward is unreachable in *every* gwt app, which silently broke SSE, streaming, and WebSocket upgrades before v3. `Hijack` over a non-hijackable writer returns `http.ErrNotSupported`, the same sentinel `http.ResponseController` uses.

**`server.go`**
- `App.Run(addr ...string) error` — derives its context from `signal.NotifyContext(os.Interrupt, syscall.SIGTERM)`. `addr` is optional and overrides `ServerConfig.Addr`, so `app.Run(":8080")` works.
- `App.RunContext(ctx context.Context, addr ...string) error` — `ctx` is used for exactly one thing: knowing *when* to begin shutdown. See Things to avoid for the `BaseContext` distinction.
- `AppConfig.BaseContext` — passthrough to `http.Server.BaseContext`, a deliberately separate mechanism from shutdown signaling.

**`config.go`**
- `type ServerConfig struct { Addr, Env string; ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout, ShutdownTimeout time.Duration }`
- `LoadServerConfig() (ServerConfig, error)` — reads `ADDR` (default `:8000`), `ENV` (`dev`), `READ_TIMEOUT` (5s), `READ_HEADER_TIMEOUT` (2s), `WRITE_TIMEOUT` (10s), `IDLE_TIMEOUT` (120s), `SHUTDOWN_TIMEOUT` (10s). Fails fast on a malformed `*_TIMEOUT` value, naming the offending variable. `New` calls this itself on every construction (see Conventions) — still exported and useful standalone for a caller that wants the resolved config before an `App` exists.
- `resolveServerConfig(c, base ServerConfig) ServerConfig` (unexported) — fills any zero-value field of `c` from `base`. `New` calls it with `base` set to `LoadServerConfig()`'s result, so the effective precedence is explicit field > environment > hardcoded default (`LoadServerConfig` itself already falls back to `defaultServerConfig()` for anything the environment didn't set). Consequence: there's no way to force a zero timeout through `AppConfig` — a zero value always reads as "unset, fall through to whatever's next in the chain." Deliberate.

**`logging.go`**
- `NewLogger(env string) *slog.Logger` — `env == "dev"` → `TextHandler`; anything else → `JSONHandler`. Both to `os.Stderr`.

**`render.go`**
- `App.LoadTemplates(fsys fs.FS, patterns ...string) error` — parses **once**, into the App's set, replacing anything loaded earlier or via `AppConfig.Templates`. `patterns` defaults to `"*.tmpl"`. Takes an `fs.FS`, so `os.DirFS("templates")` and `//go:embed` both work. Each template's name in the set is its base filename, which is what `Ctx.HTML` takes.
- `serverError(w, r, err)` (unexported) — logs and writes a generic JSON 500 for the middleware layer, which has no `Ctx`. `Recover` is its only caller. CLAUDE.md previously warned against an unexported core layer under the render helpers; **that warning was about a layer proven dead** (nothing called it). This one is live and load-bearing — the distinction is "has a real caller," not "is unexported."
- `RenderJSON`/`RenderTemplate`/`RenderServerError` **no longer exist.** Use `c.JSON`/`c.HTML`. Don't reintroduce them.

**`static.go`**
- `App.ServeStatic(urlPrefix string, fsys fs.FS, mw ...Middleware)` — registers `GET <urlPrefix>` (a subtree pattern; the caller supplies the trailing `/`, e.g. `"/static/"` — no implicit pattern rewriting) serving files from `fsys` via `http.FileServer(http.FS(fsys))`, routed through `HandleHTTP`. `fsys` works with `os.DirFS("static")` (disk) or an embedded `//go:embed static` alike, the same pattern `LoadTemplates` already uses. Wrapped in an unexported `noListingFileSystem` so a directory request without an `index.html` 404s instead of rendering a listing. Path-traversal confinement is inherited from the `fs.FS` implementation (`os.DirFS`/`embed.FS` both confine every path to their root), not re-implemented.

**`security.go`** — all three are opt-in via `Use` or route-level `mw`, never automatic: a header like `X-Frame-Options` can break a legitimate use case (intentional iframe embedding), so it isn't safe to force on every `App`.
- `App.SecureHeaders() Middleware` — `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`. Deliberately omits `X-XSS-Protection` (deprecated; the legacy filter had vulnerabilities of its own) and `Content-Security-Policy`/`Permissions-Policy` (no safe one-size-fits-all default).
- `App.MaxBodySize(n int64) Middleware` — wraps `r.Body` in `http.MaxBytesReader`. Defense-in-depth regardless of whether a reverse proxy already enforces a limit — don't assume it does. Note `Ctx.Bind` does **not** limit body size itself; this is the mechanism.
- `App.HSTS() Middleware` — `Strict-Transport-Security: max-age=31536000; includeSubDomains`. Kept separate from `SecureHeaders` deliberately: it's only correct when the deployment is actually reached over HTTPS. Gate it per-deployment (e.g. on `Env == "prod"`).

**`auth.go`**
- `App.Authentication(authenticate func(r *http.Request) (identity string, ok bool)) Middleware` — `!ok` → 401 rendered through `writeJSONError`, so it carries gwt's usual `{"error":...}` body; no `WWW-Authenticate` header, since `Authentication` doesn't know the scheme. `next` never runs. `ok` → stashes `identity` in the request context and calls `next`. Scheme-agnostic on purpose.
- `Identity(r *http.Request) (identity string, ok bool)` — package-level, for code holding only a request (middleware, `HandleHTTP` handlers). **`Ctx.Identity()` is the handler-side path** and simply delegates; the two always agree.
- `Authentication` is **route-level** middleware, passed as a trailing `mw` argument. Protecting a subset of routes is just a matter of only passing it to those routes; no grouping mechanism is needed or provided.

**`cmd/gwt-cli`**
- `gwt-cli new <name>` — flags `-module <path>`, `-port <num>`, `-force`. Emits a runnable gwt app (`main.go`, `go.mod`) demonstrating the v3 API: `app, err := gwt.New()` with no explicit `LoadServerConfig()` call (`New` loads `ADDR`/`ENV`/`*_TIMEOUT` from the environment itself — see `.env.example`), a `c.JSON` route (`/`), a `c.HTML` route (`/page`) backed by `LoadTemplates` over an **embedded** `templates/`, `ServeStatic` (`/static/`) over an **embedded** `static/` (via `fs.Sub(staticFS, "static")` — `go:embed`'s FS keeps the `static/` prefix in its paths, which a bare `staticFS` would mismatch against URL-relative lookups), a `gwt.Status` error route (`/items/{id}`), and all three security middlewares (`SecureHeaders`+`MaxBodySize` unconditionally, `HSTS` gated on `ENV=prod`). **No `Authentication` demo** — the `/admin` route and `ADMIN_USER`/`ADMIN_PASSWORD` env vars were removed as unnecessary boilerplate for a minimal starting point; `Authentication`/`Ctx.Identity` are demonstrated in `examples/basic` instead, per Principle 7. Plus the Docker set: `Dockerfile` (copies only the binary — **both `templates/` and `static/` are embedded now**), `docker-compose.yml`, `update_image`, `.env.example`, `.dockerignore`. `.in` files run through `text/template`; other files are copied verbatim. `go mod tidy` is left as a manual step so the scaffold does not depend on network access.

## Conventions

- **`New` auto-loads its `ServerConfig` from the environment and is fallible because of it — a deliberate 2026-08-04 reversal of the "New never fails" rule that stood since v2.** The user asked why `gwt.New()` alone wasn't enough to pick up `ADDR`/`ENV`/`*_TIMEOUT` — until this change, `New` never touched the environment (it only ever saw hardcoded defaults or whatever `AppConfig.Server` set explicitly), and a separate call to `LoadServerConfig()` was required to opt into env-driven config, specifically because `LoadServerConfig` fails fast on a malformed `*_TIMEOUT` and `New` could not. The fix made here: `New` calls `LoadServerConfig()` itself, propagating its error (`func New(cfg ...AppConfig) (*App, error)`), with precedence explicit `AppConfig.Server` field > environment > hardcoded default. **This does not reopen validation of the rest of `AppConfig`** — a nonsensical `BaseContext` or similar still surfaces later, at `Run`/`RunContext`, not here. Any *further* widening of what `New` validates still needs discussion first; this one exception is scoped to server-config env loading, not a general policy change (same "new decision, not a precedent" framing as Principle 4's logger exception). `LoadTemplates()` remains a separate, still-explicit call — templates were never a candidate for auto-loading, since there's no single conventional "where do templates live" the way there is for env vars.
- **Global middleware wraps the whole built handler once**, at `Handler()` build time — not per-route. So global middleware also covers the mux's own 404/405 responses; route middleware does not see those, since it only wraps the handler it's attached to. This is intentional, and it is the reason middleware stayed `net/http`-shaped (Principle 2).
- **Middleware order is fixed: `Logging` → `Recover` → anything added via `Use` (in call order) → route-level `mw` → handler.** `Logging` is outermost so it logs the true final status even when `Recover` converts a panic to a 500; `Recover` is next so it catches panics from `Use`-added middleware too.
- **`Logging` and `Recover` are the only *automatic* middleware.** `SecureHeaders`, `MaxBodySize`, `HSTS`, and `Authentication` are all opt-in. Keep it that way: a header or an auth check can break a legitimate use case in a way logging/panic-recovery never do.
- **Errors never leak to clients.** An `*HTTPError`'s `Message` is client-visible by construction; everything else renders as a generic 500 with the real cause logged. Don't put `err.Error()` in a response body.
- **Every gwt-generated error response is JSON — `{"error":"<message>"}`.** That covers handler errors, panics, `Authentication`'s 401, and the mux's own 404/405. `writeJSONError` is the shared writer for the three paths that have no `Ctx`; `defaultErrorHandler` produces the identical shape via `Ctx.JSON`, and `TestEveryErrorPathRendersTheSameShape` is what holds the two together — keep it passing when touching any error path. The two deliberate exceptions are `HandleHTTP`-mounted handlers and `ServeStatic` (built on it), which own their own output; intercepting `ServeStatic` would also mean wrapping the writer `http.FileServer` uses for its `sendfile` fast path.
- **No `c.Set`/`c.Get`.** Gin has them; gwt deliberately does not. The `Ctx` is built in the innermost frame (after all middleware), so nothing is downstream of the handler to read what `c.Set` wrote — it would be a local variable with extra steps — and a gwt-namespaced key couldn't read what foreign middleware wrote under its own key type anyway. `Ctx.Value(key)` is the read side that actually has a consumer. Adding `Set`/`Get` would repeat ISSUES.md item 29 exactly: an abstraction justified by a premise that was false from the start.
- **Shutdown hooks run LIFO** (symmetric with `defer`); startup hooks run FIFO and any error aborts startup before `ListenAndServe`. A partially-run startup list is **not** unwound — no rollback machinery, deliberately.
- Verb-first naming: `LoadTemplates`, not `TemplatesLoad`; `ServeStatic`, not `StaticServe`. File-per-role — create a new file only if a genuinely new category appears.
- Every exported symbol must have a godoc comment. `go vet ./...` must stay clean.
- slog keys are lowercase, no punctuation (`addr`, `method`, `path`, `env`, `status`). Never `"Method:"` or similar.

## Testing

Stdlib only (`testing`, `net/http/httptest`, `encoding/json`). Co-locate tests: `ctx_test.go` next to `ctx.go`, etc. Every public function should have at least a happy-path and an error-path case. `go test -race ./...` must stay clean, particularly around `Run`'s goroutine and shutdown signaling — repeated start/stop cycles under `-race` are part of the suite, not optional.

Test helpers live in `middleware_test.go` (`discardLogger`, `jsonLogger`), `render_test.go` (`templateDirFS`), and `ctx_test.go` (`serve`) — reuse them rather than re-rolling.

## Things to avoid

- **Do not** add external dependencies (router, template engine, ORM, validation lib, session lib, assertion lib). If you think one is needed, propose it in ISSUES.md and wait for approval.
- **Do not** wire `RunContext`'s shutdown-trigger context as `http.Server.BaseContext`. This is a correctness bug, not a style preference: it cancels every in-flight request's context the instant the shutdown signal fires, silently defeating graceful drain — the server still looks like it "shut down gracefully" from the outside, but every in-flight handler sees a canceled context and likely fails. `AppConfig.BaseContext` is the only knob for `BaseContext`. The shutdown context passed to `server.Shutdown()` must be derived fresh from `context.Background()`, independent of both the trigger context and `BaseContext`.
- **Do not** make `Middleware` `Ctx`-native. It breaks 404/405 coverage and stdlib interop — see Design Principle 2. This was considered and rejected during the v3 redesign, not overlooked.
- **Do not** add `c.Set`/`c.Get` — see Conventions for why they have no consumer here.
- **Do not** reintroduce `RenderJSON`/`RenderTemplate`/`RenderServerError`, or a free-function form of the `Ctx` response helpers.
- **Do not** add form validation, session storage/issuance, or CSRF protection "while in there." These are explicitly deferred. (`Authentication` is a building block, not a session implementation — don't extend it into session storage without a fresh, explicit request.)
- **Do not** add an opt-out for the default `Logging`+`Recover` wrapping, and **do not** reintroduce `Group` or any other prefix-scoped/nested route builder, without explicit user approval. Both were deliberate decisions; `Group` in particular was offered again during the v3 redesign and explicitly declined.
- **Do not** widen the "logger is implicit" exception to other dependencies. If a handler needs something else (a DB pool, a config value), pass it explicitly — capture it in a closure or hang handlers off your own struct.
- **Do not** reintroduce a functional-options constructor (`type Option func(*App)`, `WithX` functions) without explicit user approval.
- **Do not** add features "while you're in there." Scope your changes to the task.

## Where to look next

- **ISSUES.md** — the backlog, tier-ordered (foundation → rendering → scaffolding/Docker → quality → framework capabilities → static/security/auth), followed by the API-revision sections including v3. All items are checked off under "Recently completed"; the active backlog is empty and everything else is intentionally "Deferred," with reasons. Always check it before proposing new work; the user may have already decided the direction.
