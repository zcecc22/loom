package gwt

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
)

// RenderJSON marshals data as JSON and writes it to w with statusCode,
// using the App's logger. If marshaling fails, no partial body is ever
// written to w — the error is reported via RenderServerError instead.
func (a *App) RenderJSON(w http.ResponseWriter, r *http.Request, data any, statusCode int) {
	body, err := json.Marshal(data)
	if err != nil {
		a.RenderServerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if _, werr := w.Write(body); werr != nil {
		a.logger.Error("gwt: write json response failed", "err", werr, "method", r.Method, "path", r.URL.Path)
	}
}

// RenderTemplate parses tmplPath and executes it against data using
// html/template (auto-escaping, XSS-safe), rendering into an in-memory
// buffer before writing anything to w, using the App's logger. This means a
// parse or execute failure never results in a partial response — w is
// untouched until the full body is known good, at which point
// RenderServerError is invoked instead.
func (a *App) RenderTemplate(w http.ResponseWriter, r *http.Request, tmplPath string, data any, statusCode int) {
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		a.RenderServerError(w, r, err)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		a.RenderServerError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	if _, werr := buf.WriteTo(w); werr != nil {
		a.logger.Error("gwt: write template response failed", "err", werr, "method", r.Method, "path", r.URL.Path)
	}
}

// RenderServerError logs err using the App's logger and writes a 500
// response. It is the shared error path for RenderJSON, RenderTemplate,
// and Recover.
func (a *App) RenderServerError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("gwt: server error", "err", err, "method", r.Method, "path", r.URL.Path)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
