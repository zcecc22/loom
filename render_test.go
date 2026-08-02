package gwt

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderJSONHappyPath(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/items/1", nil)

	a.RenderJSON(rec, req, map[string]string{"name": "widget"}, http.StatusCreated)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json prefix", ct)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body not valid JSON: %v (%s)", err, rec.Body.String())
	}
	if got["name"] != "widget" {
		t.Fatalf("body = %v, want name=widget", got)
	}
}

func TestRenderJSONMarshalErrorFallsBackTo500(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bad", nil)

	// A channel value cannot be marshaled to JSON.
	a.RenderJSON(rec, req, map[string]any{"ch": make(chan int)}, http.StatusOK)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on marshal error", rec.Code)
	}
	if !strings.Contains(buf.String(), "server error") {
		t.Fatalf("expected marshal error to be logged, got: %s", buf.String())
	}
}

func TestRenderTemplateHTMLEscapesXSS(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "page.tmpl")
	if err := os.WriteFile(tmplPath, []byte(`<p>{{.Name}}</p>`), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	a := New(AppConfig{Logger: discardLogger()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/page", nil)

	payload := struct{ Name string }{Name: `<script>alert(1)</script>`}
	a.RenderTemplate(rec, req, tmplPath, payload, http.StatusOK)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("XSS payload was not escaped: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag, got: %s", body)
	}
}

func TestRenderTemplateExecuteErrorWritesNothingThenServerError(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "bad.tmpl")
	// {{.Missing.Field}} on a struct without that field is a runtime
	// execute error in html/template.
	if err := os.WriteFile(tmplPath, []byte(`{{.Missing.Field}}`), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bad", nil)

	a.RenderTemplate(rec, req, tmplPath, struct{ Name string }{Name: "x"}, http.StatusOK)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on execute error", rec.Code)
	}
	if !strings.Contains(buf.String(), "server error") {
		t.Fatalf("expected execute error to be logged, got: %s", buf.String())
	}
}

func TestRenderTemplateParseErrorIsServerError(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)

	a.RenderTemplate(rec, req, filepath.Join(t.TempDir(), "does-not-exist.tmpl"), nil, http.StatusOK)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on parse error", rec.Code)
	}
}

func TestRenderServerErrorLogsAndWrites500(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/err", nil)

	a.RenderServerError(rec, req, errors.New("boom"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("expected error message logged, got: %s", buf.String())
	}
}
