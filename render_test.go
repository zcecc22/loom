package gwt

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// templateDirFS writes files (name -> contents) into a temp dir and returns
// an fs.FS rooted there, for exercising LoadTemplates against real files.
func templateDirFS(t *testing.T, files map[string]string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestLoadTemplatesParsesMatchingFiles(t *testing.T) {
	dir := templateDirFS(t, map[string]string{
		"index.tmpl": `<p>{{.Name}}</p>`,
		"other.tmpl": `<p>other</p>`,
		"ignore.txt": `not a template`,
	})

	a := New(AppConfig{Logger: discardLogger()})
	if err := a.LoadTemplates(os.DirFS(dir)); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	// The default pattern is *.tmpl, so both templates are in the set and
	// each is named by its base filename.
	for _, name := range []string{"index.tmpl", "other.tmpl"} {
		if a.templates.Lookup(name) == nil {
			t.Fatalf("template %q missing from the loaded set", name)
		}
	}
	if a.templates.Lookup("ignore.txt") != nil {
		t.Fatal("ignore.txt matched the default *.tmpl pattern, want it excluded")
	}
}

func TestLoadTemplatesExplicitPatterns(t *testing.T) {
	dir := templateDirFS(t, map[string]string{
		"page.html": `<p>{{.Name}}</p>`,
	})

	a := New(AppConfig{Logger: discardLogger()})
	if err := a.LoadTemplates(os.DirFS(dir), "*.html"); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	if a.templates.Lookup("page.html") == nil {
		t.Fatal("page.html missing from the set loaded with an explicit pattern")
	}
}

func TestLoadTemplatesParseErrorIsReturned(t *testing.T) {
	dir := templateDirFS(t, map[string]string{
		"broken.tmpl": `{{ this is not valid`,
	})

	a := New(AppConfig{Logger: discardLogger()})
	err := a.LoadTemplates(os.DirFS(dir))
	if err == nil {
		t.Fatal("LoadTemplates returned nil for an unparseable template")
	}
	if a.templates != nil {
		t.Fatal("a failed LoadTemplates installed a template set anyway")
	}
}

func TestLoadTemplatesNoMatchIsError(t *testing.T) {
	a := New(AppConfig{Logger: discardLogger()})
	if err := a.LoadTemplates(os.DirFS(t.TempDir())); err == nil {
		t.Fatal("LoadTemplates returned nil when no file matched the pattern")
	}
}

func TestAppConfigTemplatesUsedWithoutLoadTemplates(t *testing.T) {
	dir := templateDirFS(t, map[string]string{"index.tmpl": `<p>{{.Name}}</p>`})

	loader := New(AppConfig{Logger: discardLogger()})
	if err := loader.LoadTemplates(os.DirFS(dir)); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	// A set built elsewhere can be handed to New directly.
	a := New(AppConfig{Logger: discardLogger(), Templates: loader.templates})
	a.GET("/page", func(c *Ctx) error {
		return c.HTML(http.StatusOK, "index.tmpl", struct{ Name string }{Name: "hi"})
	})

	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "hi") {
		t.Fatalf("body = %q, want it to contain the rendered value", rec.Body.String())
	}
}

func TestServerErrorLogsAndWrites500WithoutLeakingTheCause(t *testing.T) {
	var buf bytes.Buffer
	a := New(AppConfig{Logger: jsonLogger(&buf)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/err", nil)

	a.serverError(rec, req, errors.New("boom: secret detail"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "boom: secret detail") {
		t.Fatalf("expected the cause to be logged, got: %s", buf.String())
	}
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Fatalf("the cause leaked into the response body: %s", rec.Body.String())
	}
}
