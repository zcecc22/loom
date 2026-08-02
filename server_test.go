package gwt

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// freePort finds an OS-assigned free TCP port by briefly binding to it and
// releasing it. There is a small window where another process could steal
// the port before our server binds it, but this is the standard pattern
// for port-per-test isolation in Go test suites.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func testAddr(t *testing.T) string {
	return fmt.Sprintf("127.0.0.1:%d", freePort(t))
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s never became reachable", addr)
}

// TestRunContextGracefulShutdownWaitsForInFlightRequest is the acceptance
// test named in ISSUES.md #9. It proves three things at once: (a) an
// in-flight request is allowed to finish, with its own request context
// still live — not the BaseContext-as-shutdown-trigger bug described in
// CLAUDE.md's "Things to avoid" — (b) onShutdown hooks run in LIFO order
// only after Shutdown returns, and (c) RunContext returns nil, not
// http.ErrServerClosed.
func TestRunContextGracefulShutdownWaitsForInFlightRequest(t *testing.T) {
	addr := testAddr(t)

	started := make(chan struct{})
	proceed := make(chan struct{})
	ctxErrCh := make(chan error, 1)

	var mu sync.Mutex
	var shutdownOrder []string

	a := New(AppConfig{
		Config: Config{Addr: addr, ShutdownTimeout: 2 * time.Second},
		Logger: discardLogger(),
		OnShutdown: []func(context.Context) error{
			func(ctx context.Context) error {
				mu.Lock()
				shutdownOrder = append(shutdownOrder, "first")
				mu.Unlock()
				return nil
			},
			func(ctx context.Context) error {
				mu.Lock()
				shutdownOrder = append(shutdownOrder, "second")
				mu.Unlock()
				return nil
			},
		},
	})
	a.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-proceed
		// If RunContext's shutdown-trigger context had been wired as
		// http.Server.BaseContext, this request's context would already
		// be canceled by now (cancel() below has long since fired).
		ctxErrCh <- r.Context().Err()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- a.RunContext(ctx) }()
	waitForServer(t, addr)

	reqDone := make(chan struct{})
	var respStatus int
	var reqErr error
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		reqErr = err
		if resp != nil {
			respStatus = resp.StatusCode
			resp.Body.Close()
		}
		close(reqDone)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	cancel() // trigger shutdown while the request is in-flight
	time.Sleep(100 * time.Millisecond)
	close(proceed) // let the in-flight handler finish

	select {
	case err := <-ctxErrCh:
		if err != nil {
			t.Fatalf("in-flight request's context was canceled by shutdown trigger: %v (this is the BaseContext bug CLAUDE.md warns about)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never reached context check")
	}

	select {
	case <-reqDone:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight request never completed")
	}
	if reqErr != nil {
		t.Fatalf("in-flight request failed: %v", reqErr)
	}
	if respStatus != http.StatusOK {
		t.Fatalf("in-flight request status = %d, want 200 (should complete normally, not be aborted)", respStatus)
	}

	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("RunContext returned error = %v, want nil on clean graceful shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunContext never returned")
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"second", "first"}
	if len(shutdownOrder) != len(want) {
		t.Fatalf("shutdownOrder = %v, want %v", shutdownOrder, want)
	}
	for i := range want {
		if shutdownOrder[i] != want[i] {
			t.Fatalf("shutdownOrder = %v, want %v (LIFO)", shutdownOrder, want)
		}
	}
}

func TestServerReadHeaderTimeoutDropsStalledConnection(t *testing.T) {
	addr := testAddr(t)
	a := New(AppConfig{
		Config: Config{Addr: addr, ReadHeaderTimeout: 150 * time.Millisecond},
		Logger: discardLogger(),
	})
	a.Get("/", func(w http.ResponseWriter, r *http.Request) {})

	ctx, cancel := context.WithCancel(context.Background())
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- a.RunContext(ctx) }()
	waitForServer(t, addr)
	t.Cleanup(func() {
		cancel()
		<-runErrCh
	})

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send an incomplete request line and never terminate the headers.
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	start := time.Now()
	_, readErr := conn.Read(buf)
	elapsed := time.Since(start)

	if readErr == nil {
		t.Fatal("expected the stalled connection to be closed by ReadHeaderTimeout, got a successful read instead")
	}
	if elapsed > time.Second {
		t.Fatalf("connection took %v to close, want well under 1s (ReadHeaderTimeout=150ms)", elapsed)
	}
}

func TestPanicInOnStartHookBecomesError(t *testing.T) {
	addr := testAddr(t)
	a := New(AppConfig{
		Config: Config{Addr: addr},
		Logger: discardLogger(),
		OnStart: []func(context.Context) error{
			func(ctx context.Context) error {
				panic("start boom")
			},
		},
	})

	err := a.RunContext(context.Background())
	if err == nil {
		t.Fatal("RunContext returned nil, want error from panicking onStart hook")
	}
	if !strings.Contains(err.Error(), "start boom") {
		t.Fatalf("error = %v, want it to mention the panic value", err)
	}
}

func TestPanicInOnShutdownHookBecomesJoinedError(t *testing.T) {
	addr := testAddr(t)
	a := New(AppConfig{
		Config: Config{Addr: addr, ShutdownTimeout: time.Second},
		Logger: discardLogger(),
		OnShutdown: []func(context.Context) error{
			func(ctx context.Context) error {
				panic("shutdown boom")
			},
		},
	})
	a.Get("/", func(w http.ResponseWriter, r *http.Request) {})

	ctx, cancel := context.WithCancel(context.Background())
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- a.RunContext(ctx) }()
	waitForServer(t, addr)

	cancel()

	select {
	case err := <-runErrCh:
		if err == nil {
			t.Fatal("RunContext returned nil, want error from panicking onShutdown hook")
		}
		if !strings.Contains(err.Error(), "shutdown boom") {
			t.Fatalf("error = %v, want it to mention the panic value", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunContext never returned")
	}
}

// TestRunContextRepeatedStartStop exercises Run's goroutine and shutdown
// signaling repeatedly, the scenario ISSUES.md #17 calls out for -race
// coverage.
func TestRunContextRepeatedStartStop(t *testing.T) {
	for i := 0; i < 5; i++ {
		addr := testAddr(t)
		a := New(AppConfig{Config: Config{Addr: addr, ShutdownTimeout: time.Second}, Logger: discardLogger()})
		a.Get("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

		ctx, cancel := context.WithCancel(context.Background())
		runErrCh := make(chan error, 1)
		go func() { runErrCh <- a.RunContext(ctx) }()
		waitForServer(t, addr)

		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			t.Fatalf("iteration %d: request failed: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("iteration %d: status = %d, want 200", i, resp.StatusCode)
		}

		cancel()
		select {
		case err := <-runErrCh:
			if err != nil {
				t.Fatalf("iteration %d: RunContext error = %v", i, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d: RunContext never returned", i)
		}
	}
}

func TestRunContextReturnsNilNotErrServerClosed(t *testing.T) {
	addr := testAddr(t)
	a := New(AppConfig{Config: Config{Addr: addr, ShutdownTimeout: time.Second}, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- a.RunContext(ctx) }()
	waitForServer(t, addr)

	cancel()
	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("RunContext() = %v, want nil (not http.ErrServerClosed)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunContext never returned")
	}
}
