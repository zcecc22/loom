# gwt — Go Web Toolkit

[![Go Reference](https://pkg.go.dev/badge/github.com/zcecc22/go-web-toolkit.svg)](https://pkg.go.dev/github.com/zcecc22/go-web-toolkit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26.5%2B-00ADD8?logo=go&logoColor=white)](go.mod)

`gwt` is a zero-dependency, stdlib-only toolkit that eliminates boilerplate around routing, middleware, server lifecycle, configuration, and JSON/HTML rendering for `net/http` applications. It is deliberately **not a framework**: there's no router DSL beyond what stdlib's `http.ServeMux` already gives you, no ORM, no dependency-injection container. It's a thin layer that makes writing a small, correct, deployable Go web service fast, without hiding `net/http` from you.

```go
package main

import (
	"log"

	gwt "github.com/zcecc22/go-web-toolkit"
)

func main() {
	app, err := gwt.New()
	if err != nil {
		log.Fatal(err)
	}

	app.GET("/", func(c *gwt.Ctx) error {
		return c.JSON(200, gwt.H{"hello": "world"})
	})

	if err := app.Run(); err != nil {
		app.Logger().Error("server exited", "err", err)
	}
}
```

That's a complete, working service: structured request logging, panic recovery, graceful shutdown, and env-based config loading are all active with zero further setup. New to gwt? The fastest start is `gwt-cli new` (see [Scaffolding](#scaffolding-a-new-project) below); to understand the pieces it wires together, read the [Tutorial](#tutorial-building-an-items-api) or browse [examples/basic](examples/basic) for a single-file tour of every construct in the API.

## Contents

- [Install](#install)
- [Scaffolding a new project](#scaffolding-a-new-project)
- [Why gwt](#why-gwt)
- [Comparison to alternatives](#comparison-to-alternatives)
- [Tutorial: building an items API](#tutorial-building-an-items-api)
- [API reference](#api-reference)
- [Idiomatic usage](#idiomatic-usage)
- [Contributing](#contributing)
- [Testing](#testing)
- [License](#license)
- [Project docs](#project-docs)

## Install

```
go get github.com/zcecc22/go-web-toolkit
```

The import path is the module path above; the package name is `gwt` (they don't match — that's intentional, see [CLAUDE.md](CLAUDE.md)).

Requires **Go 1.26.5 or newer** — that's the floor `go.mod` declares. (The route-pattern syntax used throughout this README, `GET /items/{id}`, depends on `http.ServeMux` enhancements that landed in Go 1.22; 1.26.5 is simply the actual minimum this module supports.)

## Scaffolding a new project

The standard way to start a new gwt app — a runnable project, not just a library import:

```
go install github.com/zcecc22/go-web-toolkit/cmd/gwt-cli@latest

gwt-cli new myapp
cd myapp
go mod tidy
go run .
```

(From inside a checkout of this repo, `go run ./cmd/gwt-cli new myapp` works too, without installing anything.)

`gwt-cli new <name>` generates a runnable app (`main.go`, `go.mod`) demonstrating the core of the API:

- `app, err := gwt.New()` — no explicit config-loading call; `New` reads `ADDR`/`ENV`/`*_TIMEOUT` from the environment itself
- `GET /` — `c.JSON`
- `GET /page` — `c.HTML` from an embedded template set
- `GET /items/{id}` — returning `c.Error(404, ...)` for anything but `1`
- `GET /static/*` — `ServeStatic` over an embedded `static/style.css`
- All three security middlewares: `SecureHeaders`+`MaxBodySize` applied globally, `HSTS` gated on `ENV=prod`

...plus a full Docker deployment set:

- **`Dockerfile`** — multi-stage build (`golang:1.26.5` → `alpine:latest`, `CGO_ENABLED=0`); both `templates/` and `static/` are embedded in the binary, so only the binary itself is copied into the runtime image
- **`docker-compose.yml`** — single service, `env_file: .env`, `restart: unless-stopped`
- **`update_image`** — local rebuild-and-restart deploy script
- **`.env.example`** — one line per `ServerConfig` field
- **`.dockerignore`**

Flags: `-module <path>` (Go module path, default the project name), `-port <num>` (default `8000`), `-force` (overwrite an existing non-empty directory). Flags go before the name:

```
gwt-cli new -module example.com/myapp -port 9000 myapp
```

The scaffold keeps `Authentication` out of the generated app on purpose — see [Authentication](#authentication) below, or [examples/basic](examples/basic), for a worked example.

## Why gwt

- **Nothing to wire up.** `gwt.New()` alone gives you a logged, panic-safe, env-configured server. No middleware stack to assemble by hand.
- **Handlers return errors.** `func(c *gwt.Ctx) error` — return an error and the framework renders it. No `if err != nil { writeErrorAndReturn }` in every handler.
- **Stdlib routing, not a reinvented one.** Route patterns are exactly Go 1.22+ `http.ServeMux` syntax (`GET /items/{id}`), read with `c.Param("id")`.
- **Middleware is plain `net/http`.** `func(http.Handler) http.Handler`, so anything written for the standard library drops straight in.
- **Flat routing.** No nested route groups or prefix builders to reason about — every route is registered with its full path, in one place.
- **Graceful shutdown that's actually correct.** In-flight requests finish before the process exits; the shutdown trigger is never conflated with request context cancellation (a common bug this toolkit specifically avoids — see [CLAUDE.md](CLAUDE.md)).
- **Deploy-ready out of the box.** `gwt-cli new` scaffolds a runnable app plus a complete Docker deployment setup.
- **Zero dependencies.** Everything is `net/http`, `html/template`, `encoding/json`, `log/slog`. `go.mod` has no `require` lines.

## Comparison to alternatives

| | gwt | gin / echo | chi | `net/http` alone |
|---|---|---|---|---|
| Dependencies | zero | several (own router + helpers) | zero | zero |
| Routing | stdlib `http.ServeMux` | custom router | own lightweight router | stdlib `http.ServeMux` |
| Middleware shape | plain `net/http` | framework-specific | plain `net/http` | plain `net/http` |
| Request/response helpers | `Ctx` (JSON, HTML, errors, binding) | yes, framework-specific | none built in | none |
| Server lifecycle, config, scaffolding | yes | bring your own | bring your own | bring your own |

Where `gwt` sits: closer to `chi` than to `gin`/`echo` — no reinvented routing, no framework-specific middleware type — but with the request/response conveniences, config loading, graceful shutdown, and a project scaffolder that `chi` and bare `net/http` leave to you. If you want a router DSL with groups, wildcards beyond stdlib, or a large built-in middleware ecosystem, `gin` or `echo` are the better fit; `gwt` deliberately doesn't compete on that axis (see "Why gwt" above and [CLAUDE.md](CLAUDE.md)'s design principles for what was left out on purpose).

## Tutorial: building an items API

This walks through building a small `/items` service from nothing, introducing one gwt concept at a time. Each step is a complete, runnable program — start a new `main.go` and grow it as you go, or treat it as a guided read of [examples/basic](examples/basic). (If you started from `gwt-cli new` instead, most of this is already wired up — read on to see what each piece does.)

### 1. A server that does nothing yet

```go
package main

import (
	"log"

	gwt "github.com/zcecc22/go-web-toolkit"
)

func main() {
	app, err := gwt.New()
	if err != nil {
		log.Fatal(err)
	}
	app.Run()
}
```

Run it (`go run .`) and it's already a real server on `:8000` — request logging, panic recovery, and env-based config loading are all on by default, there's just nothing registered yet. `Ctrl-C` shuts it down gracefully: in-flight requests (there are none yet) get a chance to finish first.

### 2. Add a route with a path parameter

```go
type Item struct {
	Name string `json:"name"`
}

var items = map[string]Item{"1": {Name: "widget"}}

app.GET("/items/{id}", func(c *gwt.Ctx) error {
	item, ok := items[c.Param("id")]
	if !ok {
		return c.Error(404, "no such item")
	}
	return c.JSON(200, item)
})
```

`"/items/{id}"` is plain `http.ServeMux` pattern syntax; `c.Param("id")` reads the matched wildcard. Returning `c.Error(404, "no such item")` is the whole error path — no manual `w.WriteHeader` or `json.NewEncoder` needed:

```
curl localhost:8000/items/1    # {"name":"widget"}
curl localhost:8000/items/999  # {"error":"no such item"}
```

### 3. Accept writes

```go
app.POST("/items/{id}", func(c *gwt.Ctx) error {
	var in Item
	if err := c.Bind(&in); err != nil {
		return err // malformed JSON -> 400, automatically
	}
	items[c.Param("id")] = in
	return c.JSON(201, in)
})
```

`c.Bind` decodes the JSON body; a malformed body becomes a 400 without any extra code on your part — just return the error `Bind` gives you.

```
curl -XPOST -d '{"name":"gadget"}' localhost:8000/items/2
```

### 4. Render an HTML page alongside the JSON API

Add `templates/index.tmpl`:

```html
<!doctype html>
<title>Items</title>
<p>{{.Count}} item(s) in the store.</p>
```

Embed and load it, then add a route that renders it:

```go
import "embed"

//go:embed templates
var templatesFS embed.FS

// in main(), after gwt.New():
if err := app.LoadTemplates(templatesFS, "templates/*.tmpl"); err != nil {
	log.Fatal(err)
}

app.GET("/", func(c *gwt.Ctx) error {
	return c.HTML(200, "index.tmpl", gwt.H{"Count": len(items)})
})
```

Templates are parsed once at startup, not per request, and `c.HTML` auto-escapes output via `html/template` — safe against XSS by construction.

### 5. Turn on security headers

```go
app.Use(app.SecureHeaders()) // X-Content-Type-Options, X-Frame-Options, Referrer-Policy
```

`SecureHeaders` is opt-in — wire it with `Use` once, right after `New`, and it applies to every route. There's no separate config-loading step to add here: `gwt.New()` back in step 1 already reads `ADDR`, `ENV`, and the `*_TIMEOUT` variables from the environment on its own, failing fast if one is malformed.

### Putting it together

The five steps above compose into one program:

```go
package main

import (
	"embed"
	"log"

	gwt "github.com/zcecc22/go-web-toolkit"
)

type Item struct {
	Name string `json:"name"`
}

var items = map[string]Item{"1": {Name: "widget"}}

//go:embed templates
var templatesFS embed.FS

func main() {
	app, err := gwt.New()
	if err != nil {
		log.Fatal(err)
	}

	if err := app.LoadTemplates(templatesFS, "templates/*.tmpl"); err != nil {
		log.Fatal(err)
	}
	app.Use(app.SecureHeaders())

	app.GET("/items/{id}", func(c *gwt.Ctx) error {
		item, ok := items[c.Param("id")]
		if !ok {
			return c.Error(404, "no such item")
		}
		return c.JSON(200, item)
	})

	app.POST("/items/{id}", func(c *gwt.Ctx) error {
		var in Item
		if err := c.Bind(&in); err != nil {
			return err
		}
		items[c.Param("id")] = in
		return c.JSON(201, in)
	})

	app.GET("/", func(c *gwt.Ctx) error {
		return c.HTML(200, "index.tmpl", gwt.H{"Count": len(items)})
	})

	if err := app.Run(); err != nil {
		app.Logger().Error("server exited", "err", err)
	}
}
```

From here, the [API reference](#api-reference) below covers everything this tutorial didn't touch: authentication, static files, custom error handling, and more.

## API reference

### Handlers and `Ctx`

```go
type Handler func(*Ctx) error
type H map[string]any
```

Every route handler takes a `*Ctx` and returns an `error`. `Ctx` carries the request and response plus helpers for both directions. Its `W` (`http.ResponseWriter`) and `R` (`*http.Request`) fields are exported on purpose — `Ctx` is a convenience layer, never a wall. Anything it doesn't cover is one field access away.

**Reading the request**

| Method | Does |
|---|---|
| `c.Param(name)` | Path wildcard — `"/items/{id}"` → `c.Param("id")` |
| `c.Query(name)` | URL query parameter, `""` if absent |
| `c.QueryDefault(name, def)` | Query parameter, or `def` if absent or empty |
| `c.FormValue(name)` | Form value (stdlib semantics — consults the query string too) |
| `c.GetHeader(name)` | Request header |
| `c.Bind(&v)` | Decode the JSON body; a malformed body is a **400**, not a 500 |
| `c.Context()` | The request's `context.Context` |
| `c.Value(key)` | Read a value stashed by middleware via `context.WithValue` |
| `c.Identity()` | The identity set by `Authentication`, and whether one was set |
| `c.Logger()` | The App's `*slog.Logger` |

**Writing the response**

| Method | Does |
|---|---|
| `c.JSON(status, data)` | Marshal and write JSON |
| `c.String(status, s)` | Write text **literally** — a stray `%` is safe |
| `c.Stringf(status, format, args...)` | Write formatted text |
| `c.HTML(status, name, data)` | Execute a loaded template by name |
| `c.NoContent(status)` | Status only, empty body |
| `c.Redirect(status, url)` | 3xx redirect |
| `c.SetHeader(k, v)` | Set a response header (before writing) |
| `c.Written()` | Whether a response has already been committed |
| `c.Error(status, message...)` | Build an `*HTTPError` — see [Errors](#errors) |
| `c.ErrorCause(status, err)` | Build an `*HTTPError` wrapping a cause — see [Errors](#errors) |

`String` and `Stringf` are split rather than one variadic function so `go vet` can check format strings: with a single `String(status, format string, args ...any)`, vet infers a formatting wrapper and flags every non-constant argument, like `c.String(200, "hello "+user)`. The `f` suffix is what makes vet check the format instead of complaining about the call.

`c.JSON` and `c.HTML` both render fully into memory before touching the response, so a marshal or template-execute failure never emits a partial body — you get a clean 500 instead.

> **Note:** gwt does not provide gin's `c.Set`/`c.Get`. Middleware here is plain `net/http` and runs *outside* the `Ctx`, so nothing downstream of a handler could read what `c.Set` wrote. Use `context.WithValue` in middleware and `c.Value(key)` in the handler.

### Errors

```go
type HTTPError struct {
	Code    int
	Message string // client-visible
	Err     error  // logged, never sent
}

func (c *Ctx) Error(code int, message ...string) error
func (c *Ctx) ErrorCause(code int, err error) error
```

`Error` and `ErrorCause` are `Ctx` methods, like every other response-related helper — every real call site already has a `c` in scope, since they're only ever called from inside a handler.

Returning a non-nil error from a handler hands the response to the App's error handler. If an `*HTTPError` appears anywhere in the error chain, its status and message are used. Anything else becomes a **500 with a generic body** — the real error is logged and never reaches the client.

```go
return c.Error(404)                    // 404 {"error":"Not Found"}
return c.Error(404, "no such item")    // 404 {"error":"no such item"}
return c.ErrorCause(404, err)          // 404 {"error":"Not Found"}, err logged
return errors.New("db unreachable")    // 500 {"error":"Internal Server Error"}, logged
return fmt.Errorf("loading: %w", c.Error(404, "gone")) // 404 — errors.As walks the chain
```

Client errors (4xx) log at `WARN`, server errors (5xx) at `ERROR`, so a stream of bad URLs can't flood your error log.

### Error shape

**Every error gwt generates is JSON: `{"error":"<message>"}` with `Content-Type: application/json`.** That holds no matter which layer produced it, so a client can parse one shape without special-casing:

| Situation | Response |
|---|---|
| Handler returned an error | `404 {"error":"no such item"}` |
| Handler panicked | `500 {"error":"Internal Server Error"}` |
| `Authentication` rejected the request | `401 {"error":"Unauthorized"}` |
| No route matched the path | `404 {"error":"Not Found"}` |
| Route exists, wrong method | `405 {"error":"Method Not Allowed"}` + `Allow` header |

The 405's `Allow` header is preserved exactly as stdlib computes it — rewriting the body doesn't cost you the machine-readable part.

**Two deliberate exceptions**, both cases where you asked for a raw handler and should get its output verbatim:

- **`HandleHTTP` handlers** own their responses entirely. A mounted third-party handler's error format is its own business.
- **`ServeStatic`** is built on `HandleHTTP`, so a missing asset still returns `http.FileServer`'s plain-text 404. That's deliberate beyond consistency: intercepting it would mean wrapping the writer that `http.FileServer` uses for its `sendfile` fast path, making every file served slower to change the format of responses no browser reads.

If you want a different shape, `AppConfig.ErrorHandler` covers handler errors, and ordinary middleware covers the rest.

Override the whole behavior with `AppConfig.ErrorHandler`:

```go
app, err := gwt.New(gwt.AppConfig{
	ErrorHandler: func(c *gwt.Ctx, err error) {
		if c.Written() {
			return // the handler already responded; don't corrupt it
		}
		_ = c.JSON(500, gwt.H{"detail": "something went wrong"})
	},
})
```

That `c.Written()` check matters because `return c.JSON(...)` is the normal idiom: a handler can respond in full and *then* return an error (a write failure, a deferred cleanup). The default handler makes the same check.

### Constructing an App

```go
func New(cfg ...AppConfig) (*App, error)
```

`New` builds a ready `*App` — logged, panic-safe, listening on the default address, with zero further setup. It also loads its server config from the environment itself (`ADDR`, `ENV`, and the `*_TIMEOUT` variables — see Configuration below), which is the only thing that can make it fail: a malformed `*_TIMEOUT` value is a fail-fast error. `cfg` is variadic only so `gwt.New()` with no argument keeps working for the zero-config case — passing more than one `AppConfig` is meaningless and only the first is used.

There's no functional-options layer here: `AppConfig` is a plain struct. Set the fields you care about, leave the rest zero:

```go
type AppConfig struct {
	Server       ServerConfig // Addr, Env, and timeouts — see Configuration below
	Logger       *slog.Logger
	Templates    *template.Template
	ErrorHandler func(*Ctx, error)
	OnStart      []func(context.Context) error
	OnShutdown   []func(context.Context) error
	BaseContext  func(net.Listener) context.Context
}
```

| Field | Effect |
|---|---|
| `Server` | Address, environment, and timeouts. Precedence: this field (where non-zero) > the environment > the hardcoded default. |
| `Logger` | Override the default logger |
| `Templates` | A pre-parsed template set, if you'd rather not use `LoadTemplates` |
| `ErrorHandler` | Override how a returned error becomes a response |
| `OnStart` | Startup hooks, run in order before the server starts serving; first error aborts startup |
| `OnShutdown` | Shutdown hooks, run in **LIFO** order during graceful shutdown |
| `BaseContext` | Passthrough to `http.Server.BaseContext` |

Three independent examples (not meant to run together):

```go
app, err := gwt.New() // reads ADDR/ENV/*_TIMEOUT from the environment; hardcoded default for anything unset
```

```go
// An explicit Server field wins over whatever the environment set.
app, err := gwt.New(gwt.AppConfig{Server: gwt.ServerConfig{Addr: ":9000"}})
```

```go
app, err := gwt.New(gwt.AppConfig{
	Logger: myLogger,
	OnShutdown: []func(context.Context) error{
		func(ctx context.Context) error { return db.Close() },
	},
})
```

`App` implements `http.Handler`, so it drops straight into `httptest.NewServer(app)` or nests inside another handler. `App.Handler()` returns the fully-built handler if you want to run your own `http.Server` instead of `Run`.

#### Configuration

```go
type ServerConfig struct {
	Addr, Env string
	ReadTimeout, ReadHeaderTimeout, WriteTimeout time.Duration
	IdleTimeout, ShutdownTimeout                 time.Duration
}

func LoadServerConfig() (ServerConfig, error)
```

This is what `New` calls internally on every construction — you don't normally need to call it yourself. `LoadServerConfig` reads from the environment and fails fast (naming the offending variable) if a `*_TIMEOUT` value is set but unparsable:

| Env var | Default |
|---|---|
| `ADDR` | `:8000` |
| `ENV` | `dev` |
| `READ_TIMEOUT` | `5s` |
| `READ_HEADER_TIMEOUT` | `2s` |
| `WRITE_TIMEOUT` | `10s` |
| `IDLE_TIMEOUT` | `120s` |
| `SHUTDOWN_TIMEOUT` | `10s` |

It stays exported for the case where you want the resolved config *before* an `App` exists — e.g. to build your own logger from `cfg.Env` ahead of time, or to log the resolved address before binding it:

```go
cfg, err := gwt.LoadServerConfig()
if err != nil {
	log.Fatal(err)
}
logger := gwt.NewLogger(cfg.Env)
app, err := gwt.New(gwt.AppConfig{Server: cfg, Logger: logger})
```

### Server lifecycle

```go
app.Run(addr ...string) error
app.RunContext(ctx context.Context, addr ...string) error
```

`Run` is a convenience that triggers shutdown on `SIGINT`/`SIGTERM`. `RunContext` triggers shutdown when `ctx` is done. `addr` is optional and overrides `ServerConfig.Addr` when given, so `app.Run(":8080")` works directly.

Either way: on shutdown, in-flight requests get up to `ServerConfig.ShutdownTimeout` to finish before the server closes; `AppConfig.OnShutdown` hooks then run in **LIFO** order (last registered, first run — symmetric with `defer`), each one always running regardless of earlier failures. `AppConfig.OnStart` hooks run in **FIFO** order before the server starts serving; the first error aborts startup. A panicking hook becomes an error, not a process crash. Both functions return `nil` on a clean shutdown, never `http.ErrServerClosed`.

### Routing

```go
app.GET(pattern string, h Handler, mw ...Middleware)
app.POST(pattern string, h Handler, mw ...Middleware)
app.PUT(pattern string, h Handler, mw ...Middleware)
app.PATCH(pattern string, h Handler, mw ...Middleware)
app.DELETE(pattern string, h Handler, mw ...Middleware)
app.OPTIONS(pattern string, h Handler, mw ...Middleware)
app.Handle(method, pattern string, h Handler, mw ...Middleware)
app.HandleHTTP(method, pattern string, h http.Handler, mw ...Middleware)
```

Patterns are exactly stdlib `http.ServeMux` syntax — `"/items/{id}"`, read with `c.Param("id")`. There is no route grouping or prefixing; write the full path at each call site.

`Handle` is the generic form: pass the verb as a string (works for anything without a dedicated method, e.g. `"TRACE"`), or `""` to match any verb. There's no dedicated `HEAD` method because stdlib already matches HEAD requests against a `GET` pattern.

`HandleHTTP` mounts a plain `http.Handler`, bypassing `Ctx` entirely — the escape hatch for third-party handlers, reverse proxies, and anything already written against `net/http`.

```go
app.OPTIONS("/items/{id}", func(c *gwt.Ctx) error {
	c.SetHeader("Allow", "GET, PUT, DELETE, OPTIONS")
	return c.NoContent(204)
})

app.HandleHTTP("GET", "/metrics", promHandler) // any http.Handler
```

### Middleware

```go
type Middleware func(http.Handler) http.Handler

func Chain(mw ...Middleware) Middleware
app.Logging() Middleware
app.Recover() Middleware
app.Use(mw ...Middleware)
```

Middleware is plain `net/http`, **not** `Ctx`-based. That's deliberate: global middleware also covers the mux's own 404/405 responses, and those requests never match a route — so there's no `Ctx` to give them. Keeping the stdlib shape also means any middleware written for `net/http` composes with gwt directly.

`Chain(A, B, C)` composes so declared order equals request-flow order: `A` runs first. `app.Logging()` and `app.Recover()` are the built-in middleware `New` already wires for you; call them yourself only if you're doing something unusual. `app.Use(mw...)` adds anything beyond the defaults — it appends, stacked *after* logging/recovery (closer to the handler), so a panic or early exit inside your own middleware is still caught and logged.

**Fixed request-flow order:** `Logging → Recover → Use-added (in call order) → route-level mw → handler`.

```go
app, err := gwt.New()
app.Use(app.SecureHeaders())

app.GET("/admin", adminHandler, requireRole("admin"))
```

> Call `Use` before serving starts. The middleware chain is composed once, on the first request, and cached — a `Use` after that point has no effect.

### Templates

```go
app.LoadTemplates(fsys fs.FS, patterns ...string) error
c.HTML(status int, name string, data any) error
```

Templates are parsed **once**, not per request. `LoadTemplates` takes any `fs.FS`, so both on-disk and embedded templates work. `patterns` defaults to `"*.tmpl"`.

```go
// From disk
app.LoadTemplates(os.DirFS("templates"))

// Embedded in the binary — no template files needed at runtime
//go:embed templates
var templatesFS embed.FS
app.LoadTemplates(templatesFS, "templates/*.tmpl")
```

Each template's name in the set is its base filename, which is what `c.HTML` takes:

```go
app.GET("/page", func(c *gwt.Ctx) error {
	return c.HTML(200, "index.tmpl", gwt.H{"Name": "world"})
})
```

Rendering uses `html/template`, so output is contextually auto-escaped — safe against XSS by construction. The template is executed into a buffer before anything is written, so an execute failure yields a clean 500 rather than a half-written page.

### Static files

```go
app.ServeStatic(urlPrefix string, fsys fs.FS, mw ...Middleware)
```

Serves files from `fsys` at `urlPrefix` (a subtree pattern — supply the trailing `/` yourself, e.g. `"/static/"`) via `http.FileServer`. `fsys` works with a disk directory or an embedded filesystem alike — the same `fs.FS` pattern `LoadTemplates` uses. Unlike a bare `http.FileServer`, a directory request without an `index.html` 404s instead of rendering a directory listing — everything else behaves like stdlib, including path-traversal confinement (inherited from the `fs.FS` implementation, not reimplemented).

```go
// From disk
app.ServeStatic("/static/", os.DirFS("static"))
// GET /static/app.css        -> serves ./static/app.css
// GET /static/images/        -> 404, no listing (unless images/index.html exists)
// GET /static/images/pic.png -> serves it normally

// Embedded in the binary — no static files needed at runtime.
// go:embed keeps the "static/" prefix in the resulting FS's paths, so
// fs.Sub strips it back to the same relative shape os.DirFS produces —
// without it, a request for /static/app.css would 404 looking for
// "app.css" in an FS that only has "static/app.css".
//go:embed static
var staticFS embed.FS
staticSub, err := fs.Sub(staticFS, "static")
if err != nil {
	log.Fatal(err)
}
app.ServeStatic("/static/", staticSub)
```

Note that a disk-backed `fsys` is read relative to the working directory at request time — unlike an embedded one, it needs to ship alongside the binary.

### Security

```go
app.SecureHeaders() Middleware // X-Content-Type-Options, X-Frame-Options, Referrer-Policy
app.MaxBodySize(n int64) Middleware // caps request body size via http.MaxBytesReader
app.HSTS() Middleware // Strict-Transport-Security
```

All three are opt-in via `Use` (or route-level `mw`) — never automatic, unlike `Logging`/`Recover`. A header like `X-Frame-Options` can break a legitimate use case (intentional iframe embedding), so it isn't safe to force on every `App` by default.

`SecureHeaders` deliberately omits `X-XSS-Protection` (deprecated — the legacy filter it enabled had vulnerabilities of its own) and `Content-Security-Policy`/`Permissions-Policy` (no safe one-size-fits-all default exists — add your own if your app needs one). `HSTS` is kept separate from `SecureHeaders` on purpose: it's only correct when this deployment is actually reached over HTTPS (directly, or via a TLS-terminating reverse proxy) — turn it on deliberately per-deployment, since enabling it for a plain-HTTP deployment like local dev would be wrong, not just unnecessary.

`c.Bind` does not limit body size on its own; `MaxBodySize` is the mechanism for that.

```go
app, err := gwt.New()
app.Use(app.SecureHeaders(), app.MaxBodySize(10<<20)) // 10 MiB
if os.Getenv("ENV") == "prod" {
	app.Use(app.HSTS())
}
```

### Authentication

```go
app.Authentication(authenticate func(r *http.Request) (identity string, ok bool)) Middleware
c.Identity() (identity string, ok bool)
gwt.Identity(r *http.Request) (identity string, ok bool)
```

`Authentication` is generic and scheme-agnostic: you supply `authenticate`, which does the actual credential check (Basic auth, a bearer token, a signed cookie — whatever your app needs) and returns an identity string plus whether it's valid. On `!ok`, `Authentication` responds `401` and the handler never runs. On `ok`, the identity is stashed in the request context; read it back with `c.Identity()`.

`Authentication` is route-level middleware — pass it as a trailing argument to `GET`/`POST`/etc. to protect specific routes. There's no grouping mechanism and none is needed: protecting a subset of routes is just a matter of which registration calls you pass `Authentication(...)` to.

A worked example using HTTP Basic Auth:

```go
app.GET("/admin", func(c *gwt.Ctx) error {
	user, _ := c.Identity()
	return c.JSON(200, gwt.H{"logged_in_as": user})
}, app.Authentication(func(r *http.Request) (string, bool) {
	user, pass, ok := r.BasicAuth()
	if !ok || !validCredentials(user, pass) {
		return "", false
	}
	return user, true
}))
```

The package-level `gwt.Identity(r)` does the same thing for code that holds only a request — middleware, or a handler mounted with `HandleHTTP`.

`gwt` doesn't provide a built-in Basic/Bearer/session implementation — `authenticate` is where you plug in whichever scheme you need. This also means `gwt` doesn't issue, sign, or store sessions, and has no CSRF token mechanism; `Authentication` is a building block for that, not a session framework.

`Authentication`'s 401 uses the same JSON error shape as every other gwt error — see [Error shape](#error-shape).

### Logging

```go
func NewLogger(env string) *slog.Logger
app.Logger() *slog.Logger
c.Logger() *slog.Logger
```

`NewLogger` returns a text-handler logger for `env == "dev"`, JSON otherwise — this is what `New` uses internally if you don't supply `AppConfig.Logger`. `app.Logger()` returns the App's resolved logger; `c.Logger()` is the same logger, reachable from inside a handler.

## Idiomatic usage

- **Start with zero setup.** `gwt.New()` is a complete, safe starting point. Reach for `AppConfig` fields only when you actually need to override something.
- **Return errors instead of writing them.** `return c.Error(404, "not found")` beats writing a status and remembering to `return`. Reserve manual responses for success paths.
- **Let internal errors stay internal.** Returning a plain error gives the client a generic 500 and puts the detail in the log. That's the intended behavior — don't put `err.Error()` into a response body.
- **The logger is the one implicit dependency.** It's embedded in `App` so the built-in middleware and `c.Logger()` don't need it threaded through every call. Everything else — a DB handle, a config value, anything your handlers need — should be passed explicitly (typically via closures, see below), not bolted onto `App`.
- **Wire dependencies with closures, not a DI container.** There's no framework-level way to inject a database handle into a handler. Build handlers as closures over what they need:

  ```go
  func itemsGet(store *Store) gwt.Handler {
  	return func(c *gwt.Ctx) error {
  		item, err := store.Get(c.Param("id"))
  		if err != nil {
  			return c.ErrorCause(404, err)
  		}
  		return c.JSON(200, item)
  	}
  }

  app.GET("/items/{id}", itemsGet(store))
  ```

- **Use `Use` for concerns that apply to every route** — security headers, request IDs, CORS, whatever your app needs beyond logging/recovery — applied once, right after `New`. Use route-level `mw` (the trailing arguments to `GET`/`POST`/etc.) instead for anything that shouldn't apply everywhere — `Authentication` is the main example: wiring it into `Use` would lock out every route, including ones that should stay public (health checks, static assets), so it's designed to be passed only to the specific routes that need it.
- **Keep routes flat.** There's no `Group`. If you have `/api/v1/...`-style paths, write the full pattern string at each call site — a small amount of repetition in exchange for never having to reason about how prefixes and middleware compose across nesting levels.
- **Test handlers directly against `App`.** `App` implements `http.Handler`, so `httptest.NewRecorder()` + `app.ServeHTTP(rec, req)` — or `httptest.NewServer(app)` for a real listener — is all you need; no framework-specific test harness.
- **Prefer `RunContext` over `Run` when embedding gwt in something larger** (e.g. a supervisor that manages multiple services) — pass your own cancelable context instead of relying on OS signal handling.

## Contributing

This repo documents its own conventions in unusual depth: [CLAUDE.md](CLAUDE.md) lays out the design principles and internal API surface, and [ISSUES.md](ISSUES.md) is a dated history of what's shipped, including alternatives that were tried and explicitly rejected. Before proposing a change — especially anything that looks like an obvious addition (a functional-options constructor, route groups, `c.Set`/`c.Get`, a new dependency) — check ISSUES.md's "Deferred" section; several of the obvious ideas have already been tried once and reverted, with the reasoning recorded there.

Pull requests are welcome. Please keep `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` clean, and add tests alongside the code they cover (see [Testing](#testing)).

## Testing

```
go test ./...
go test -race ./...
```

The whole toolkit is tested with the standard library only (`testing`, `net/http/httptest`) — no assertion library, no mocking framework.

## License

MIT — see [LICENSE](LICENSE).

## Project docs

- **[CLAUDE.md](CLAUDE.md)** — design principles, full internal API surface, and conventions for anyone modifying this repo.
- **[ISSUES.md](ISSUES.md)** — implementation backlog and history, including why the API looks the way it does.
- **[examples/basic](examples/basic)** — a single-file tour of every construct in the API.
