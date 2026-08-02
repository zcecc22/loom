package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// templatesFS embeds every file under templates/, both the plain source
// templates (*.in) and the verbatim-copied files (e.g. templates/index.tmpl,
// itself an html/template interpreted by the generated app at runtime, not
// by gwt-cli at generation time).
//
//go:embed all:templates
var templatesFS embed.FS

// scaffoldData is the template data available to every .in file during
// generation.
type scaffoldData struct {
	Name      string // project directory / binary / image name
	Module    string // Go module path for the new project
	Port      int    // port the generated app listens on
	GwtModule string // this toolkit's module path, for the go.mod require/import lines
}

// manifestEntry describes one file to emit. SrcPath is relative to
// templates/, DstPath is relative to the new project's root. IsTemplate
// marks SrcPath as a text/template (by convention named *.in) to be
// executed against scaffoldData; otherwise it is copied byte-for-byte.
type manifestEntry struct {
	SrcPath    string
	DstPath    string
	IsTemplate bool
}

// manifest is the explicit, hand-maintained list of files gwt-cli new
// emits — no magic path transformations (e.g. stripping ".in" implicitly);
// every output file is listed here by name.
var manifest = []manifestEntry{
	{"main.go.in", "main.go", true},
	{"go.mod.in", "go.mod", true},
	{"gitignore.in", ".gitignore", true},
	{"dockerignore.in", ".dockerignore", true},
	{"Dockerfile.in", "Dockerfile", true},
	{"docker-compose.yml.in", "docker-compose.yml", true},
	{"update_image.in", "update_image", true},
	{"env.example.in", ".env.example", true},
	{"templates/index.tmpl", "templates/index.tmpl", false},
}

// scaffold writes a new project at destDir using data. If force is false
// and destDir already exists and is non-empty, scaffold refuses to
// overwrite it.
func scaffold(destDir string, data scaffoldData, force bool) error {
	if !force {
		if entries, err := os.ReadDir(destDir); err == nil && len(entries) > 0 {
			return fmt.Errorf("%s already exists and is not empty (use -force to overwrite)", destDir)
		}
	}

	for _, entry := range manifest {
		srcPath := "templates/" + entry.SrcPath
		body, err := fs.ReadFile(templatesFS, srcPath)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", srcPath, err)
		}

		out := body
		if entry.IsTemplate {
			tmpl, err := template.New(entry.SrcPath).Parse(string(body))
			if err != nil {
				return fmt.Errorf("parse template %s: %w", srcPath, err)
			}
			var buf strings.Builder
			if err := tmpl.Execute(&buf, data); err != nil {
				return fmt.Errorf("execute template %s: %w", srcPath, err)
			}
			out = []byte(buf.String())
		}

		dstPath := filepath.Join(destDir, entry.DstPath)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", dstPath, err)
		}

		perm := os.FileMode(0o644)
		if entry.DstPath == "update_image" {
			perm = 0o755
		}
		if err := os.WriteFile(dstPath, out, perm); err != nil {
			return fmt.Errorf("write %s: %w", dstPath, err)
		}
	}

	return nil
}
