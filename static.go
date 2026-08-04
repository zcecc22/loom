package gwt

import (
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// ServeStatic registers a GET handler at urlPrefix (a subtree pattern —
// supply the trailing "/" yourself, e.g. "/static/") that serves files
// from fsys using http.FileServer. fsys works with a disk directory
// (os.DirFS("static")) or an embedded filesystem (//go:embed static)
// alike — the same fs.FS pattern LoadTemplates already uses. Unlike a
// bare http.FileServer, this never renders a directory listing: a
// directory request is served only if that directory contains an
// index.html; otherwise it 404s. mw wraps this handler only, not the
// whole App — see Use for whole-app middleware.
func (a *App) ServeStatic(urlPrefix string, fsys fs.FS, mw ...Middleware) {
	fileServer := http.FileServer(noListingFileSystem{http.FS(fsys)})
	a.HandleHTTP(http.MethodGet, urlPrefix, http.StripPrefix(urlPrefix, fileServer), mw...)
}

// noListingFileSystem wraps an http.FileSystem so that opening a
// directory without an index.html returns os.ErrNotExist (a 404) instead
// of letting http.FileServer render a directory listing. Path-traversal
// confinement is unaffected — it's inherited from the underlying fs.FS
// implementation (os.DirFS or embed.FS both confine every path to their
// root), not re-implemented here.
type noListingFileSystem struct {
	fs http.FileSystem
}

func (nfs noListingFileSystem) Open(name string) (http.File, error) {
	f, err := nfs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	if stat.IsDir() {
		indexName := strings.TrimSuffix(name, "/") + "/index.html"
		index, err := nfs.fs.Open(indexName)
		if err != nil {
			f.Close()
			return nil, os.ErrNotExist
		}
		index.Close()
	}

	return f, nil
}
