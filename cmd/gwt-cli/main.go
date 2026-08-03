// Command gwt-cli scaffolds new gwt projects. See CLAUDE.md's "cmd/gwt-cli"
// section for the full list of files it generates.
package main

import (
	"flag"
	"fmt"
	"os"
)

// gwtModulePath is this toolkit's own module path, used as the import and
// go.mod require path in generated projects.
const gwtModulePath = "github.com/zcecc22/go-web-toolkit"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "new":
		if err := runNew(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "gwt-cli:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "gwt-cli: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `gwt-cli — gwt project scaffolder

Usage:
  gwt-cli new <name> [-module <path>] [-port <num>] [-force]

Flags:
  -module string   Go module path for the new project (default "<name>")
  -port int        port the generated app listens on (default 8000)
  -force           overwrite <name> if it already exists and is non-empty`)
}

func runNew(args []string) error {
	fset := flag.NewFlagSet("new", flag.ContinueOnError)
	module := fset.String("module", "", "Go module path for the new project")
	port := fset.Int("port", 8000, "port the generated app listens on")
	force := fset.Bool("force", false, "overwrite the destination directory if it already exists")
	if err := fset.Parse(args); err != nil {
		return err
	}

	if fset.NArg() != 1 {
		return fmt.Errorf("expected exactly one project name, got %d", fset.NArg())
	}
	name := fset.Arg(0)

	mod := *module
	if mod == "" {
		mod = name
	}

	data := scaffoldData{
		Name:      name,
		Module:    mod,
		Port:      *port,
		GwtModule: gwtModulePath,
	}

	if err := scaffold(name, data, *force); err != nil {
		return err
	}

	fmt.Printf("gwt-cli: created %s\n", name)
	fmt.Printf("  cd %s\n", name)
	fmt.Println("  go mod tidy")
	fmt.Println("  go run .")
	return nil
}
