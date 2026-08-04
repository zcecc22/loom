package gwt

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeStaticServesFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.ServeStatic("/static/", dir)

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "body{}" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "body{}")
	}
}

func TestServeStaticDirectoryWithoutIndexReturns404(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nodir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.ServeStatic("/static/", dir)

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/nodir/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (no directory listing without index.html)", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "file.txt") {
		t.Fatalf("response leaked a directory listing: %s", rec.Body.String())
	}
}

func TestServeStaticDirectoryWithIndexServesIt(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "withindex")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.ServeStatic("/static/", dir)

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/withindex/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "<h1>hi</h1>" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "<h1>hi</h1>")
	}
}

func TestServeStaticPathTraversalCannotEscapeDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "static")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("TOP SECRET"), 0o644); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	a := mustNew(t, AppConfig{Logger: discardLogger()})
	a.ServeStatic("/static/", dir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/../secret.txt", nil)
	// Simulate a raw, uncleaned request target the way a real HTTP client
	// could send one on the wire (httptest.NewRequest's URL parsing
	// already normalizes "..", so set it explicitly to bypass that).
	req.URL.Path = "/static/../secret.txt"
	a.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "TOP SECRET") {
		t.Fatalf("path traversal leaked file outside the served directory: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
