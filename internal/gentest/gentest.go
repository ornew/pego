// Package gentest holds what the tests of the parser engine and its generators share: the grammars
// and inputs of the test corpus, and the generation, fixture forms and test modules of generated
// parsers, so the tests in each package run the same corpus the same way.
package gentest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ornew/pego/internal/gen/gogen"
)

func init() {
	gogen.SetRuntimeSource(os.DirFS(filepath.Join(moduleRoot(), "runtime")))
}

// moduleRoot returns the root directory of this module, the nearest directory above the working
// directory that has a go.mod; tests run in the directory of their package.
var moduleRoot = sync.OnceValue(func() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("gentest: no go.mod above the working directory")
		}
		dir = parent
	}
})

// path returns the path of name, a slash-separated path relative to the directory of this package.
func path(t testing.TB, name string) string {
	t.Helper()
	return filepath.Join(moduleRoot(), "internal", "gentest", filepath.FromSlash(name))
}
