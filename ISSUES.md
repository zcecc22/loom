# ISSUES.md — gwt v2 backlog

Prioritized implementation backlog, tier-ordered. **All 22 Tier 1–5 items are implemented and passing** (see "Recently completed" below, resolved 2026-08-02), plus a same-day **API revision** (items 23–26: embedded logger, default middleware, `Group` removed — see that section for the full reasoning). The active backlog is currently empty — everything not yet built is intentionally out of scope, listed under "Deferred" with reasons.

Every issue has a **Done when** criterion so completion is unambiguous. Every issue respects the hard stdlib-only rule from `CLAUDE.md` — no external dependencies without explicit user approval. Issue IDs are stable across tiers so future references don't shift as items get resolved.

---

## Recently completed

**Tier 1 — Foundation** — all resolved 2026-08-02.

1. ✅ `doc.go` added with a package-level doc comment and minimal usage example. `go build ./...` and `go vet ./...` clean.
2. ✅ `Middleware` type + `Chain()` shipped in `middleware.go`. `TestChainOrderMatchesRequestFlow` proves `Chain(A, B, C)` runs A's pre-handler logic first.
3. ✅ `App` + `New(opts ...Option) *App` + `WithAddr`/`WithEnv`/`WithLogger`/`WithMiddleware` shipped in `app.go`. `TestNewWithZeroOptions` confirms zero-option construction never panics/errors. **Revised 2026-08-02** — see "API revision" below: `WithMiddleware` was removed in favor of `App.Use`, and `New` now unconditionally wires default middleware.
4. ✅ `Get`/`Post`/`Put`/`Patch`/`Delete`/`Handle` shipped in `router.go`, wired onto stdlib `http.ServeMux` method+wildcard patterns. `TestVerbHelpersRouteToCorrectHandlerAndPathValue` covers all five verbs plus `r.PathValue`.
5. ↩️ **Reverted 2026-08-02.** `Group(prefix, fn, mw...)` originally shipped in `router.go` with `joinPattern` handling prefix-joining (`TestGroupNestedThreeLevelsPrefixAndMiddlewareOrder` covered a three-level-deep nesting case). Removed entirely per user request — see "API revision" below. Routing is flat now: explicit verbs plus the generic `Handle`.
6. ✅ `Config` + `LoadConfig() (Config, error)` shipped in `config.go`, fail-fast on malformed `*_TIMEOUT` values (a deliberate v1→v2 signature break — v1 silently ignored bad input). `TestLoadConfigMalformedDurationEachField` covers all five duration fields.
7. ✅ `NewLogger(env)` shipped in `logging.go` — text handler for `"dev"`, JSON handler otherwise. `TestNewLoggerHandlerTypes` covers both branches.
8. ✅ `Logging()` and `Recover()` middleware shipped in `middleware.go`. `TestLoggingLogsExactlyOnceOnPanic` and `TestRecoverCatchesPanicAndLogs500` cover the panic path; recommended composition is `Chain(Logging(logger), Recover(logger))`. **Revised 2026-08-02** — see "API revision" below: both are now zero-arg `App` methods (`a.Logging()`, `a.Recover()`) wired automatically by `New`, not free functions the caller assembles.
9. ✅ `Run()`/`RunContext(ctx)` shipped in `server.go` with graceful shutdown and LIFO/FIFO hooks. `TestRunContextGracefulShutdownWaitsForInFlightRequest` is the acceptance test: it proves an in-flight request's context is untouched by the shutdown trigger (the `BaseContext` bug named in `CLAUDE.md` never manifests), `onShutdown` hooks fire LIFO only after `Shutdown` returns, and `RunContext` returns `nil`, not `http.ErrServerClosed`.
10. ✅ Server timeout hardening: all four `http.Server` timeout fields are non-zero by default (`defaultConfig()`). `TestServerReadHeaderTimeoutDropsStalledConnection` proves a stalled connection is dropped well under 1s.

**Tier 2 — Rendering helpers** — all resolved 2026-08-02.

11. ✅ `RenderJSON` shipped in `render.go`. `TestRenderJSONMarshalErrorFallsBackTo500` covers the marshal-error path (delegates to `RenderServerError`, no partial write). **Revised 2026-08-02** — see "API revision" below: now `App.RenderJSON`, a method, not a free function.
12. ✅ `RenderTemplate` shipped in `render.go`, buffered into `bytes.Buffer` before ever touching the real `ResponseWriter` (v1's hardening, carried forward). `TestRenderTemplateHTMLEscapesXSS` proves auto-escaping; `TestRenderTemplateExecuteErrorWritesNothingThenServerError` proves zero bytes are written to `w` on an execute failure. **Revised 2026-08-02**: now `App.RenderTemplate`, a method.
13. ✅ `RenderServerError` shipped in `render.go` as the shared error path for `RenderJSON`, `RenderTemplate`, and `Recover`. `TestRenderServerErrorLogsAndWrites500` covers direct use; the other two tests above cover the indirect paths. **Revised 2026-08-02**: now `App.RenderServerError`, a method.

**Tier 3 — Scaffolding & Docker deployment** — resolved 2026-08-02.

14. ✅ `cmd/gwt-cli new <name>` shipped: explicit manifest (`scaffold.go`) + `//go:embed all:templates`, generating `main.go`/`go.mod` with a `RenderJSON` demo route and a `RenderTemplate` demo route (`templates/index.tmpl`), plus `Dockerfile` (multi-stage `golang:1.24.4`→`alpine:latest`, `CGO_ENABLED=0`), `docker-compose.yml`, `update_image`, `.dockerignore`, `.env.example` — all verbatim-modeled on the sibling project `go-pkm`'s deployment files. Flags `-module`, `-port`, `-force`. `TestGeneratedMainGoParsesAsValidGo` covers the `go/parser` check; `TestGeneratedProjectBuildsAndVets` wires a local `replace` and runs the real toolchain (`go mod tidy`, `go build ./...`, `go vet ./...`) hermetically. The Dockerfile's multi-stage build mechanics were additionally verified manually with a real `docker build` + `docker run` (a stdlib-only stand-in was used for that manual check, since this module isn't yet published anywhere fetchable — the `go mod download` step inside the Dockerfile is otherwise identical to go-pkm's already-proven, in-production pattern). One scope deviation from the original issue text: the Dockerfile copies only `templates/` into the runtime image, not `static/` — static file serving is deferred (see below), so no `static/` directory exists in the scaffold.

**Tier 4 — Quality/testing** — all resolved 2026-08-02.

15. ↩️ **Reverted 2026-08-02.** `joinPattern` edge cases originally covered by `TestJoinPatternEdgeCases` (root, trailing slash, empty prefix, no-leading-slash pattern, nested prefixes, bare-slash prefix) and `TestGroupExactRootVsSubtreePattern` (exact root vs. subtree matching). Both tests and `joinPattern` itself were deleted along with `Group` — see "API revision" below.
16. ✅ `TestEndToEndGlobalGroupRouteMiddlewareOrder` exercised a 3-level route tree with global + group + route middleware in one assertion chain. **Revised 2026-08-02** — see "API revision" below: renamed `TestEndToEndDefaultAndUseMiddlewareOrder`, now covers default (`Logging`/`Recover`) + `Use`-added + route-level middleware, since `Group` no longer exists.
17. ✅ `go test -race ./...` passes with zero warnings (verified with `CGO_ENABLED=1`, run 3x for flakiness), including `TestRunContextRepeatedStartStop`'s 5-iteration start/stop cycle.
18. ✅ `go vet ./...` clean; every exported symbol across `gwt` and `cmd/gwt-cli` has a godoc comment (`Config`'s individual fields were the one gap, since fixed).
19. ✅ `BenchmarkChain` added to `middleware_test.go`, result recorded in a comment: `7.533 ns/op, 0 B/op, 0 allocs/op` (amd64, go1.24.4) — Chain's closures are fully escape-analyzed away in this hot path.
20. ✅ `examples/basic/main.go` + `examples/basic/main_test.go`'s `Example` function shipped, using `New` and (in `main()`) `Run`. `go test ./...` executes it and its `Output:` comment matches. **Revised 2026-08-02**: rewritten to flat routes (`Group` removed) and zero-arg `New()` — see "API revision" below.

**Tier 5 — Framework capabilities** — all resolved 2026-08-02.

21. ✅ `WithReadTimeout`/`WithReadHeaderTimeout`/`WithWriteTimeout`/`WithIdleTimeout`/`WithShutdownTimeout` shipped in `app.go`. `TestGranularTimeoutOptionsEachOverrideOnlyOwnField` and `TestWithConfigThenGranularOverrideWinsOnlyThatField` cover both the standalone and layered-over-`WithConfig` cases.
22. ✅ Panic-safe hook execution (`callHook` in `server.go`) converts a panicking `onStart`/`onShutdown` hook into an error via `errors.Join`, never a process crash. `TestPanicInOnStartHookBecomesError` and `TestPanicInOnShutdownHookBecomesJoinedError` cover both hook kinds.

Issue IDs are stable across tiers so old references don't shift as items get resolved.

---

## API revision — 2026-08-02

After the initial implementation shipped, the user tried the API and asked for two simplifications, resolved the same day via three rounds of clarifying questions (final answers recorded here so the reasoning isn't lost):

23. ✅ **Logger embedded in `App`; render helpers become methods.** `RenderJSON`/`RenderTemplate`/`RenderServerError` are now `App` methods (`a.RenderJSON(w, r, data, status)`, no `logger` parameter) instead of free functions. The old free functions were removed entirely (not kept alongside the methods) — chosen over keeping both to avoid two ways to do the same thing. Implementation: the method bodies delegate to unexported core functions (`renderJSON`/`renderTemplate`/`renderServerError`) that still take `logger` explicitly, so `Recover` (which doesn't always have a full `App` in scope in the same way) and the methods can share one implementation. Supersedes issues 11–13.
   Done when: `go test ./...` passes with every render call site converted to method form (`app_test.go`, `render_test.go`, `examples/basic`, `cmd/gwt-cli/templates/main.go.in`), and `go vet ./...` is clean.

24. ✅ **`Logging`/`Recover` become zero-arg `App` methods; `New` wires them unconditionally; `App.Use` replaces `WithMiddleware`.** `a.Logging()`/`a.Recover()` read `a.logger` instead of taking it as a parameter. `New(opts ...Option) *App` now always sets the App's global middleware to `Chain(a.Logging(), a.Recover())` — there is no way to construct an `App` without them, and no opt-out was requested or added. `WithMiddleware` (an `Option`, construction-time only) was removed and replaced by `App.Use(mw ...Middleware)` (a plain method, callable any time after `New`), which appends further middleware *after* the defaults (closer to the handler), never replacing them. Fixed order: `Logging` → `Recover` → `Use`-added (in call order) → route-level `mw` → handler. Supersedes issue 8's original free-function form and issue 3's `WithMiddleware`.
   Done when: `TestNewAutoWiresLoggingAndRecoverWithNoSetup` proves `gwt.New()` alone logs and recovers panics with zero further setup; `TestUseStacksAfterDefaultsNotBefore` proves a panic inside `Use`-added middleware is still caught by the default `Recover` (i.e. `Recover` wraps outside anything added via `Use`); `TestUseWrapsWholeHandlerOnce` and `TestHandlerCoversMuxNotFound` cover `Use`'s append behavior and 404/405 coverage the same way `WithMiddleware`'s tests used to.

25. ✅ **`App.Logger()` accessor added.** A direct consequence of hiding the logger: `main()` no longer holds its own `logger` variable, so it needs some way to log `Run`/`RunContext`'s returned error. `App.Logger() *slog.Logger` returns the resolved logger. Used in `examples/basic/main.go` and `cmd/gwt-cli/templates/main.go.in`'s post-`Run` error handling.
   Done when: `TestLoggerReturnsResolvedLogger` covers both the `WithLogger`-supplied case and the default-resolved case.

26. ↩️ **`Group` removed entirely — routing is flat.** The type, its `Get`/`Post`/`Put`/`Patch`/`Delete`/`Handle`/`Group` methods, and the `joinPattern` helper (all originally issue 5) were deleted. The user wanted explicit verb methods plus one generic verb method taking the HTTP verb as an argument instead of a prefix-scoped nested route builder — `Handle(method, pattern, h, mw...)` already was that generic method, so this was a straight deletion, not a redesign. `examples/basic/main.go` and `cmd/gwt-cli/templates/main.go.in` were rewritten to flat routes (full paths at each call site) as part of this.
   Done when: `go build ./...`/`go vet ./...` are clean with `Group` gone from the whole module (library, tests, example, scaffold templates), and `TestEndToEndDefaultAndUseMiddlewareOrder` (router_test.go) covers the middleware-ordering guarantee that `TestEndToEndGlobalGroupRouteMiddlewareOrder` used to cover, minus the group tier.

Two things were explicitly *not* added, per Design Principle 7 (no speculative abstractions) — don't add either without a fresh, explicit request: an opt-out for the default `Logging`+`Recover` wrapping, and any replacement for `Group` (prefix helpers, nested groups, etc.).

---

## Deferred (explicitly out of scope — not oversights)

- **Static file serving** (`ServeStaticFiles` equivalent of v1's `helpers.go`). Reason: not requested this pass — JSON/template rendering was explicitly requested, static serving wasn't, so they're kept separate rather than bundled in under the same tier. Revisit as a small addition to `render.go` or a new `static.go` once core is stable.
- **Form parsing/validation.** Reason: not part of v1 either; genuinely new scope with no prior art to restore, deserving its own design pass.
- **Sessions, cookie auth, CSRF.** Reason: security-sensitive surface deserves its own dedicated design pass rather than being bolted onto a routing/rendering rewrite.
- **`RequestID` middleware, `App.Routes()` route introspection, `WithBaseContext` beyond the minimal passthrough already specified.** Reason: speculative — no confirmed use case yet. Per Design Principle 7 ("no speculative abstractions"), these land only when something real needs them. (Note: `WithBaseContext` itself — the minimal passthrough to `http.Server.BaseContext` — is implemented in `app.go`/`server.go`, since it's part of the documented API surface; only *extensions* beyond it are deferred.)
- **CORS middleware.** Reason: not requested; noted explicitly so it isn't added by accident once the middleware chain exists and looks easy to extend.
- **Registry push / CI-CD for scaffolded Docker images.** Reason: go-pkm's `update_image` script (the reference pattern) is purely local build-and-restart with no registry step; v2's scaffold matches that scope deliberately and does not add CI/CD.

---

## Dependency policy reminder

Any new issue added here must be scoped to **stdlib only**. If implementing one turns out to genuinely require an external package, stop and ask — the zero-dependency rule is the defining property of this project and changing it is a conscious decision, not an implementation detail.
