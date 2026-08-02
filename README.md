# gwt — Go Web Toolkit

`gwt` is a zero-dependency, stdlib-only toolkit that eliminates boilerplate around routing, middleware, server lifecycle, configuration, and JSON/HTML rendering for `net/http` applications. It is deliberately **not a framework**: there's no router DSL beyond what stdlib's `http.ServeMux` already gives you, no ORM, no dependency-injection container. It's a thin layer that makes writing a small, correct, deployable Go web service fast, without hiding `net/http` from you.

```go
package main

import (
	"net/http"

	gwt "git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git"
)

func main() {
	app := gwt.New()

	app.Get("/", func(w http.ResponseWriter, r *http.Request) {
		app.RenderJSON(w, r, map[string]string{"hello": "world"}, http.StatusOK)
	})

	if err := app.Run(); err != nil {
		app.Logger().Error("server exited", "err", err)
	}
}
```

That's a complete, working service: structured request logging, panic recovery, and graceful shutdown are all active with zero further setup.

## Install

```
go get git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git
```

The import path is the module path above; the package name is `gwt` (they don't match — that's intentional, see [CLAUDE.md](CLAUDE.md)).

## Why gwt

- **Nothing to wire up.** `gwt.New()` alone gives you a logged, panic-safe server. No middleware stack to assemble by hand.
- **Stdlib routing, not a reinvented one.** Route patterns are exactly Go 1.22+ `http.ServeMux` syntax (`GET /items/{id}`), read path params with stdlib's own `r.PathValue`.
- **Flat routing.** No nested route groups or prefix builders to reason about — every route is registered with its full path, in one place.
- **Graceful shutdown that's actually correct.** In-flight requests finish before the process exits; the shutdown trigger is never conflated with request context cancellation (a common bug this toolkit specifically avoids — see [CLAUDE.md](CLAUDE.md)).
- **Deploy-ready out of the box.** `gwt-cli new` scaffolds a runnable app plus a complete Docker deployment setup.
- **Zero dependencies.** Everything is `net/http`, `html/template`, `encoding/json`, `log/slog`. `go.mod` has no `require` lines.

## Feature overview

| Area | What you get |
|---|---|
| Routing | `Get`/`Post`/`Put`/`Patch`/`Delete`/`Handle`, stdlib `ServeMux` patterns + path params |
| Middleware | Automatic logging + panic recovery, `Use` for anything more |
| Rendering | `RenderJSON`, `RenderTemplate` (XSS-safe, buffered), `RenderServerError` |
| Lifecycle | `Run`/`RunContext` with graceful shutdown, startup/shutdown hooks |
| Config | Env-based `LoadConfig`, fail-fast on malformed values |
| Scaffolding | `gwt-cli new` — runnable app + Dockerfile/compose/deploy script |

## Quick start

```go
app := gwt.New()

app.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	app.RenderJSON(w, r, map[string]string{"id": id}, http.StatusOK)
})

app.Post("/items", func(w http.ResponseWriter, r *http.Request) {
	var in Item
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		app.RenderServerError(w, r, err)
		return
	}
	app.RenderJSON(w, r, in, http.StatusCreated)
})

app.Run()
```

Run it, then:

```
curl localhost:8000/items/42        # {"id":"42"}
curl -XPOST -d '{"name":"widget"}' localhost:8000/items
```

## API reference

### Constructing an App

```go
func New(cfg ...AppConfig) *App
```

`New` never fails and never validates — it applies `cfg` (if given) over sane defaults and returns a ready `*App`. It **unconditionally** wires request logging and panic recovery as the App's middleware; there's no way to construct an `App` without them. `cfg` is variadic only so `gwt.New()` with no argument keeps working for the zero-config case — passing more than one `AppConfig` is meaningless and only the first is used.

There's no functional-options layer here: `AppConfig` is a plain struct. Set the fields you care about, leave the rest zero:

```go
type AppConfig struct {
	Config      Config // Addr, Env, and timeouts — see Configuration below
	Logger      *slog.Logger
	OnStart     []func(context.Context) error
	OnShutdown  []func(context.Context) error
	BaseContext func(net.Listener) context.Context
}
```

| Field | Effect |
|---|---|
| `Config` | Address, environment, and timeouts — any zero-value field falls back to the default for that field. Typically `LoadConfig()`'s result. |
| `Logger` | Override the default logger |
| `OnStart` | Startup hooks, run in order before the server starts serving; first error aborts startup |
| `OnShutdown` | Shutdown hooks, run in **LIFO** order during graceful shutdown |
| `BaseContext` | Passthrough to `http.Server.BaseContext` |

Three independent examples (not meant to run together):

```go
app := gwt.New() // every default
```

```go
app := gwt.New(gwt.AppConfig{Config: gwt.Config{Addr: ":9000"}})
```

```go
app := gwt.New(gwt.AppConfig{
	Logger: myLogger,
	OnShutdown: []func(context.Context) error{
		func(ctx context.Context) error { return db.Close() },
	},
})
```

`App` implements `http.Handler`, so it drops straight into `httptest.NewServer(app)` or nests inside another handler. `App.Handler()` returns the fully-built handler if you want to run your own `http.Server` instead of `Run`.

### Routing

```go
app.Get(pattern string, h http.HandlerFunc, mw ...Middleware)
app.Post(pattern string, h http.HandlerFunc, mw ...Middleware)
app.Put(pattern string, h http.HandlerFunc, mw ...Middleware)
app.Patch(pattern string, h http.HandlerFunc, mw ...Middleware)
app.Delete(pattern string, h http.HandlerFunc, mw ...Middleware)
app.Handle(method, pattern string, h http.Handler, mw ...Middleware)
```

Patterns are exactly stdlib `http.ServeMux` syntax — `"/items/{id}"`, read with `r.PathValue("id")`. `Handle` is the generic form: pass the verb as a string (works for anything, e.g. `"OPTIONS"`), or `""` to match any verb. There is no route grouping or prefixing — write the full path at each call site.

### Middleware

```go
type Middleware func(http.Handler) http.Handler

func Chain(mw ...Middleware) Middleware
app.Logging() Middleware
app.Recover() Middleware
app.Use(mw ...Middleware)
```

`Chain(A, B, C)` composes so declared order equals request-flow order: `A` runs first. `app.Logging()` and `app.Recover()` are the built-in middleware `New` already wires for you; call them yourself only if you're doing something unusual (e.g. running a sub-handler outside `Run`). `app.Use(mw...)` is how you add anything beyond the defaults — it appends, stacked *after* logging/recovery (closer to the handler), so a panic or early exit inside your own middleware is still caught and logged.

**Fixed request-flow order:** `Logging → Recover → Use-added (in call order) → route-level mw → handler`.

```go
app := gwt.New()
app.Use(authMiddleware)

app.Get("/admin", adminHandler, requireRoleMiddleware("admin"))
```

### Rendering

```go
app.RenderJSON(w, r, data any, statusCode int)
app.RenderTemplate(w, r, tmplPath string, data any, statusCode int)
app.RenderServerError(w, r, err error)
```

`RenderJSON` marshals and writes `data`; a marshal error is reported via `RenderServerError` instead of a partial write. `RenderTemplate` parses `tmplPath` and executes it via `html/template` (auto-escaping — safe against XSS by construction) into an in-memory buffer *before* writing anything to `w`, so a parse or execute failure never produces a partial response. `RenderServerError` logs `err` and writes a 500; it's the shared error path for the two render helpers above and for `Recover`.

### Server lifecycle

```go
app.Run() error
app.RunContext(ctx context.Context) error
```

`Run` is a convenience that triggers shutdown on `SIGINT`/`SIGTERM`. `RunContext` triggers shutdown when `ctx` is done. Either way: on shutdown, in-flight requests get up to `Config.ShutdownTimeout` to finish before the server closes; `AppConfig.OnShutdown` hooks then run in **LIFO** order (last registered, first run — symmetric with `defer`), each one always running regardless of earlier failures. `AppConfig.OnStart` hooks run in **FIFO** order before the server starts serving; the first error aborts startup. A panicking hook becomes an error, not a process crash. Both functions return `nil` on a clean shutdown, never `http.ErrServerClosed`.

### Configuration

```go
type Config struct {
	Addr, Env string
	ReadTimeout, ReadHeaderTimeout, WriteTimeout time.Duration
	IdleTimeout, ShutdownTimeout                 time.Duration
}

func LoadConfig() (Config, error)
```

`LoadConfig` reads from the environment and fails fast (naming the offending variable) if a `*_TIMEOUT` value is set but unparsable:

| Env var | Default |
|---|---|
| `ADDR` | `:8000` |
| `ENV` | `dev` |
| `READ_TIMEOUT` | `5s` |
| `READ_HEADER_TIMEOUT` | `2s` |
| `WRITE_TIMEOUT` | `10s` |
| `IDLE_TIMEOUT` | `120s` |
| `SHUTDOWN_TIMEOUT` | `10s` |

```go
cfg, err := gwt.LoadConfig()
if err != nil {
	log.Fatal(err)
}
app := gwt.New(gwt.AppConfig{Config: cfg})
```

### Logging

```go
func NewLogger(env string) *slog.Logger
app.Logger() *slog.Logger
```

`NewLogger` returns a text-handler logger for `env == "dev"`, JSON otherwise — this is what `New` uses internally if you don't supply `AppConfig.Logger`. `app.Logger()` returns the App's resolved logger, useful for logging `Run`'s returned error when you don't hold a separate logger variable (see the Quick start example above).

## Idiomatic usage

- **Start with zero setup.** `gwt.New()` is a complete, safe starting point. Reach for `AppConfig` fields only when you actually need to override something.
- **The logger is the one implicit dependency.** It's embedded in `App` so render calls and the built-in middleware don't need it threaded through every call. Everything else — a DB handle, a config value, anything your handlers need — should be passed explicitly (typically via closures, see below), not bolted onto `App`.
- **Wire dependencies with closures, not a DI container.** There's no framework-level way to inject a database handle or similar into a handler. Build handlers as closures over what they need:

  ```go
  func itemsGet(app *gwt.App, store *Store) http.HandlerFunc {
  	return func(w http.ResponseWriter, r *http.Request) {
  		item, err := store.Get(r.PathValue("id"))
  		if err != nil {
  			app.RenderServerError(w, r, err)
  			return
  		}
  		app.RenderJSON(w, r, item, http.StatusOK)
  	}
  }

  app.Get("/items/{id}", itemsGet(app, store))
  ```

- **Use `Use` for cross-cutting concerns**, applied once, right after `New` — auth, request IDs, CORS, whatever your app needs beyond logging/recovery. Route-level `mw` (the trailing arguments to `Get`/`Post`/etc.) is for per-route behavior instead.
- **Keep routes flat.** There's no `Group`. If you have `/api/v1/...`-style paths, write the full pattern string at each call site — a small amount of repetition in exchange for never having to reason about how prefixes and middleware compose across nesting levels.
- **Test handlers directly against `App`.** `App` implements `http.Handler`, so `httptest.NewRecorder()` + `app.ServeHTTP(rec, req)` — or `httptest.NewServer(app)` for a real listener — is all you need; no framework-specific test harness.
- **Prefer `RunContext` over `Run` when embedding gwt in something larger** (e.g. a supervisor that manages multiple services) — pass your own cancelable context instead of relying on OS signal handling.

## Scaffolding a new project

```
go install git.frentzel.xyz/home/zcecc22/git/go-web-toolkit.git/cmd/gwt-cli@latest

gwt-cli new myapp
cd myapp
go mod tidy
go run .
```

(From inside a checkout of this repo, `go run ./cmd/gwt-cli new myapp` works too, without installing anything.)

`gwt-cli new <name>` generates a runnable app (`main.go`, `go.mod`, a `RenderJSON` demo route, a `RenderTemplate` demo route with `templates/index.tmpl`) plus a full Docker deployment set:

- **`Dockerfile`** — multi-stage build (`golang:1.24.4` → `alpine:latest`, `CGO_ENABLED=0`)
- **`docker-compose.yml`** — single service, `env_file: .env`, `restart: unless-stopped`
- **`update_image`** — local rebuild-and-restart deploy script
- **`.env.example`** — one line per `Config` field
- **`.dockerignore`**

Flags: `-module <path>` (Go module path, default the project name), `-port <num>` (default `8000`), `-force` (overwrite an existing non-empty directory). Flags go before the name:

```
gwt-cli new -module example.com/myapp -port 9000 myapp
```

## Testing

```
go test ./...
go test -race ./...
```

The whole toolkit is tested with the standard library only (`testing`, `net/http/httptest`) — no assertion library, no mocking framework.

## Project docs

- **[CLAUDE.md](CLAUDE.md)** — design principles, full internal API surface, and conventions for anyone modifying this repo.
- **[ISSUES.md](ISSUES.md)** — implementation backlog and history, including why the API looks the way it does.
