package engine

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ornew/pego/internal/gentest"
)

// The tests of this package compile the grammars that they generate parsers for with the engine.
func init() {
	gentest.Compile = compileForGenerator
}

// runtimeSource is the text of the runtime package, for tests that check its source.
var runtimeSource = func() string {
	paths, err := filepath.Glob(filepath.Join("..", "..", "runtime", "*.go"))
	if err != nil {
		panic(err)
	}
	var b strings.Builder
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			panic(err)
		}
		b.Write(data)
	}
	return b.String()
}()
