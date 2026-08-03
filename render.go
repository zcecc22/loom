package gwt

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

// LoadTemplates parses the templates in fsys matching patterns into the
// App's template set, which Ctx.HTML renders from. It replaces any set
// loaded earlier or supplied via AppConfig.Templates.
//
// patterns defaults to "*.tmpl" when omitted. fsys is an fs.FS, so both
// on-disk and embedded templates work:
//
//	app.LoadTemplates(os.DirFS("templates"))  // from disk
//
//	//go:embed templates
//	var templatesFS embed.FS
//	app.LoadTemplates(templatesFS, "templates/*.tmpl")  // embedded
//
// Each template's name within the set is its base filename, which is what
// Ctx.HTML takes: LoadTemplates(os.DirFS("templates")) then
// c.HTML(200, "index.tmpl", data).
//
// Parsing happens here, once, rather than per request. Templates are
// html/template, so output is contextually auto-escaped.
func (a *App) LoadTemplates(fsys fs.FS, patterns ...string) error {
	if len(patterns) == 0 {
		patterns = []string{"*.tmpl"}
	}

	tmpl, err := template.ParseFS(fsys, patterns...)
	if err != nil {
		return fmt.Errorf("gwt: load templates: %w", err)
	}

	a.templates = tmpl
	return nil
}

// serverError logs err and writes a generic 500, in the same JSON shape
// the default error handler uses. It exists for the middleware layer,
// which is plain net/http and so has no Ctx to render through — Recover
// is its one caller.
//
// Like defaultErrorHandler, it never puts err in the response body, and
// like defaultErrorHandler it refuses to write onto a response that is
// already committed — a handler can respond in full and only then panic,
// and appending a second body to that response would corrupt it. The
// always-on Logging middleware is what makes the check possible: Recover
// runs inside it, so w is the *statusWriter that knows.
func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("gwt: server error", "err", err, "method", r.Method, "path", r.URL.Path)

	if sw, ok := w.(*statusWriter); ok && sw.wroteHeader {
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	if _, werr := w.Write([]byte(`{"error":"Internal Server Error"}`)); werr != nil {
		a.logger.Error("gwt: write error response failed", "err", werr, "method", r.Method, "path", r.URL.Path)
	}
}
