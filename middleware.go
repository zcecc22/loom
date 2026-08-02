package gwt

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"
)

// Middleware wraps an http.Handler to produce a new http.Handler, typically
// adding behavior before and/or after calling the wrapped handler.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares so that declared order equals request-flow
// order: Chain(A, B, C)(h) is equivalent to A(B(C(h))) — A's pre-handler
// logic runs first (outermost) and its post-handler logic runs last.
func Chain(mw ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		wrapped := final
		// Apply last-to-first so the first middleware ends up as the
		// outermost wrapper, e.g. for [A, B]: wrapped = A(B(final)).
		for i := len(mw) - 1; i >= 0; i-- {
			wrapped = mw[i](wrapped)
		}
		return wrapped
	}
}

// statusWriter records the status code written to an http.ResponseWriter
// so Logging can report it after the handler returns.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(status int) {
	if sw.wroteHeader {
		return
	}
	sw.status = status
	sw.wroteHeader = true
	sw.ResponseWriter.WriteHeader(status)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	return sw.ResponseWriter.Write(b)
}

// Logging returns a Middleware that logs method, path, status, and duration
// for every request that passes through it, exactly once, whether or not
// the wrapped handler panics, using the App's logger. For the panic case to
// be observed here rather than crashing the process, Recover must run
// inside Logging in the chain — New already wires them this way
// (Logging outermost, Recover next) as the App's default middleware.
func (a *App) Logging() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			a.logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", time.Since(start).String(),
			)
		})
	}
}

// Recover returns a Middleware that recovers from panics in the wrapped
// handler, logs the recovered value and a stack trace at Error level using
// the App's logger, and responds with a 500 via RenderServerError.
func (a *App) Recover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					a.logger.Error("panic recovered",
						"panic", fmt.Sprint(rec),
						"stack", string(debug.Stack()),
						"method", r.Method,
						"path", r.URL.Path,
					)
					a.RenderServerError(w, r, fmt.Errorf("panic: %v", rec))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
