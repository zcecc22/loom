package gwt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// Run starts the App's HTTP server and blocks until it shuts down.
// Shutdown is triggered by SIGINT or SIGTERM (via signal.NotifyContext)
// and performed gracefully, per RunContext.
//
// addr is optional and overrides ServerConfig.Addr when given, so app.Run() uses
// the configured address and app.Run(":8080") forces one. Passing more
// than one address is meaningless; only addr[0] is used.
func (a *App) Run(addr ...string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.RunContext(ctx, addr...)
}

// RunContext starts the App's HTTP server and blocks until it shuts down.
// ctx is used for exactly one thing: knowing when to begin shutdown. When
// ctx is done, RunContext calls http.Server.Shutdown with a fresh context
// derived from ServerConfig.ShutdownTimeout — independent of ctx (which may
// already be canceled) and independent of any AppConfig.BaseContext func.
//
// ctx is never wired as http.Server.BaseContext: doing so would cancel
// every in-flight request's context the instant ctx is canceled, silently
// defeating graceful drain (the server would still appear to "shut down
// gracefully" from the outside, but in-flight handlers would see a
// canceled context and likely fail). See CLAUDE.md's "Things to avoid".
//
// addr is optional and overrides ServerConfig.Addr when given, as in Run.
//
// RunContext returns nil on a clean graceful shutdown, not
// http.ErrServerClosed.
func (a *App) RunContext(ctx context.Context, addr ...string) error {
	listenAddr := a.cfg.Addr
	if len(addr) > 0 {
		listenAddr = addr[0]
	}

	if err := runOnStart(context.Background(), a.onStart); err != nil {
		return fmt.Errorf("gwt: startup hook failed: %w", err)
	}

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           a.Handler(),
		ReadTimeout:       a.cfg.ReadTimeout,
		ReadHeaderTimeout: a.cfg.ReadHeaderTimeout,
		WriteTimeout:      a.cfg.WriteTimeout,
		IdleTimeout:       a.cfg.IdleTimeout,
		BaseContext:       a.baseContextFn,
	}

	a.logger.Info("gwt: starting server", "addr", listenAddr, "env", a.cfg.Env)

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- srv.ListenAndServe()
	}()

	var serveErr error
	select {
	case serveErr = <-serveErrCh:
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			serveErr = fmt.Errorf("gwt: shutdown: %w", err)
		}
		if err := <-serveErrCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr = errors.Join(serveErr, err)
		}
	}

	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	if err := runOnShutdown(context.Background(), a.onShutdown); err != nil {
		serveErr = errors.Join(serveErr, fmt.Errorf("gwt: shutdown hook failed: %w", err))
	}

	return serveErr
}

// runOnStart executes hooks in registration order, aborting at the first
// error. A panicking hook is converted to an error rather than crashing
// the process; hooks already run are not unwound.
func runOnStart(ctx context.Context, hooks []func(context.Context) error) error {
	for _, h := range hooks {
		if err := callHook(ctx, h); err != nil {
			return err
		}
	}
	return nil
}

// runOnShutdown executes hooks in LIFO order (last registered, first run),
// symmetric with defer. Every hook runs regardless of earlier hooks'
// failures; all errors (including panics converted to errors) are joined.
func runOnShutdown(ctx context.Context, hooks []func(context.Context) error) error {
	var errs []error
	// Iterate back-to-front so the last-registered hook runs first — LIFO,
	// symmetric with defer.
	for i := len(hooks) - 1; i >= 0; i-- {
		if err := callHook(ctx, hooks[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// callHook runs a single hook, converting a panic into an error instead of
// crashing the process.
func callHook(ctx context.Context, h func(context.Context) error) error {
	var hookErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				hookErr = fmt.Errorf("gwt: hook panic: %v", rec)
			}
		}()
		hookErr = h(ctx)
	}()
	return hookErr
}
