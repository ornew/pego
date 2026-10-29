// Package gentest holds the grammars and inputs that tests of the parser engine and its generators
// share, so the tests in each package run the same corpus.
package gentest

import (
	"os"
	"path/filepath"
	"testing"
)

// path returns the path of name, a slash-separated path relative to the directory of this package,
// from any package directory of the module.
func path(t testing.TB, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "internal", "gentest", filepath.FromSlash(name))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("gentest: no go.mod above the working directory")
		}
		dir = parent
	}
}
