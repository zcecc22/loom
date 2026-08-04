package gwt

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(bytesDiscard{}, nil))
}

type bytesDiscard struct{}

func (bytesDiscard) Write(p []byte) (int, error) { return len(p), nil }

func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func TestChainOrderMatchesRequestFlow(t *testing.T) {
	var order []string

	tag := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+":before")
				next.ServeHTTP(w, r)
				order = append(order, name+":after")
			})
		}
	}

	handler := Chain(tag("A"), tag("B"), tag("C"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"A:before", "B:before", "C:before", "handler", "C:after", "B:after", "A:after"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestChainEmpty(t *testing.T) {
	called := false
	h := Chain()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !called {
		t.Fatal("handler not called through empty Chain")
	}
}

func TestRecoverCatchesPanicAndLogs500(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})

	h := a.Recover()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "panic recovered") {
		t.Fatalf("log output missing panic entry: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("log output missing recovered value: %s", buf.String())
	}
}

func TestRecoverNoPanicPassesThrough(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	h := a.Recover()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
}

func TestLoggingLogsExactlyOnceOnPanic(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})

	h := Chain(a.Logging(), a.Recover())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panics", nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var requestLogs int
	for _, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if rec["msg"] == "request" {
			requestLogs++
			if rec["status"] != float64(http.StatusInternalServerError) {
				t.Fatalf("logged status = %v, want 500", rec["status"])
			}
		}
	}
	if requestLogs != 1 {
		t.Fatalf("request logged %d times, want exactly 1", requestLogs)
	}
}

func TestLoggingLogsExactlyOnceHappyPath(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})

	h := a.Logging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/items", nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %v", len(lines), lines)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("bad log line: %v", err)
	}
	if rec["status"] != float64(http.StatusCreated) {
		t.Fatalf("status = %v, want 201", rec["status"])
	}
	if rec["method"] != http.MethodPost || rec["path"] != "/items" {
		t.Fatalf("unexpected fields: %v", rec)
	}
}

// BenchmarkChain result (amd64, AMD EPYC-Genoa, go1.24.4):
//
//	BenchmarkChain-4   	165918702	         7.533 ns/op	       0 B/op	       0 allocs/op
//
// Re-run with `go test -run '^$' -bench BenchmarkChain -benchmem .` if this
// regresses to a non-zero allocs/op — that would indicate Chain's closures
// stopped being escape-analyzed away.
func BenchmarkChain(b *testing.B) {
	noop := func(next http.Handler) http.Handler { return next }
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Chain(noop, noop, noop)(h).ServeHTTP(rec, req)
	}
}
