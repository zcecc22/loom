package main

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldGeneratesExpectedFiles(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "testapp")

	data := scaffoldData{Name: "testapp", Module: "testapp", Port: 9001, GwtModule: gwtModulePath}
	if err := scaffold(dest, data, false); err != nil {
		t.Fatalf("scaffold() error = %v", err)
	}

	want := []string{
		"main.go", "go.mod", ".gitignore", ".dockerignore", "Dockerfile",
		"docker-compose.yml", "update_image", ".env.example", "templates/index.tmpl",
		"static/style.css",
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("expected generated file %s: %v", rel, err)
		}
	}
}

func TestScaffoldRefusesNonEmptyDestWithoutForce(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "testapp")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	data := scaffoldData{Name: "testapp", Module: "testapp", Port: 8000, GwtModule: gwtModulePath}
	if err := scaffold(dest, data, false); err == nil {
		t.Fatal("scaffold() error = nil, want error for non-empty dest without -force")
	}
	if err := scaffold(dest, data, true); err != nil {
		t.Fatalf("scaffold() with force=true error = %v", err)
	}
}

func TestGeneratedMainGoParsesAsValidGo(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "testapp")
	data := scaffoldData{Name: "testapp", Module: "testapp", Port: 8000, GwtModule: gwtModulePath}
	if err := scaffold(dest, data, false); err != nil {
		t.Fatalf("scaffold() error = %v", err)
	}

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, filepath.Join(dest, "main.go"), nil, parser.AllErrors); err != nil {
		t.Fatalf("generated main.go is not valid Go: %v", err)
	}
}

// TestGeneratedProjectBuildsAndVets is the automated stand-in for ISSUES.md
// #14's "go build ./... and go vet ./... are clean" criterion: it wires
// the generated project's go.mod to this checkout via a local replace (so
// the check is hermetic and needs no network or a published module) and
// runs the real toolchain against it.
func TestGeneratedProjectBuildsAndVets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build/vet of generated project in -short mode")
	}

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Fatalf("expected repo root at %s: %v", repoRoot, err)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "testapp")
	data := scaffoldData{Name: "testapp", Module: "testapp", Port: 8000, GwtModule: gwtModulePath}
	if err := scaffold(dest, data, false); err != nil {
		t.Fatalf("scaffold() error = %v", err)
	}

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dest
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
	}

	run("go", "mod", "edit", "-replace", gwtModulePath+"="+repoRoot)
	run("go", "mod", "tidy")
	run("go", "build", "./...")
	run("go", "vet", "./...")
}

func TestRunNewRequiresExactlyOneName(t *testing.T) {
	if err := runNew([]string{}); err == nil {
		t.Fatal("runNew([]) error = nil, want error")
	}
	if err := runNew([]string{"a", "b"}); err == nil {
		t.Fatal("runNew([a b]) error = nil, want error")
	}
}

func TestRunNewDefaultsModuleToNameAndAppliesFlags(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := runNew([]string{"myapp"}); err != nil {
		t.Fatalf("runNew([myapp]) error = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "myapp", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "module myapp") {
		t.Fatalf("go.mod = %s, want module myapp (default from name)", body)
	}

	if err := runNew([]string{"-module", "example.com/custom", "-port", "9090", "-force", "myapp"}); err != nil {
		t.Fatalf("runNew with flags error = %v", err)
	}
	body, err = os.ReadFile(filepath.Join(dir, "myapp", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "module example.com/custom") {
		t.Fatalf("go.mod = %s, want module example.com/custom", body)
	}
	env, err := os.ReadFile(filepath.Join(dir, "myapp", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "ADDR=:9090") {
		t.Fatalf(".env.example = %s, want ADDR=:9090", env)
	}
}
