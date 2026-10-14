package spec_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The package directory is the test's working directory, so every file of the package is
// checked for cobra, tests included. Only the package's own files are held to importing no os.
func TestPackageImportsNoCLIFramework(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no Go files found in the package directory")
	}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: import %s: %v", name, imp.Path.Value, err)
			}
			if path == "github.com/spf13/cobra" || strings.HasPrefix(path, "github.com/spf13/cobra/") {
				t.Errorf("%s imports %s", name, path)
			}
			if path == "os" && !strings.HasSuffix(name, "_test.go") {
				t.Errorf("%s imports os; the package performs no I/O", name)
			}
		}
	}
}
