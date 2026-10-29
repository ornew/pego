package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

// The tests of this package compile the grammars that they generate parsers for with the engine.
func init() {
	gentest.Compile = compileForGenerator
	gentest.Reference = referenceProgram
}

// referenceProgram compiles src with the engine for the comparisons of generated parsers with it.
func referenceProgram(t *testing.T, src string, opts compile.Options) gentest.ReferenceParse {
	t.Helper()
	prog := compileBare(t, src, opts)
	return func(input string, bytes, recognize bool) (*value.Node, error) {
		unit := CodePoints
		if bytes {
			unit = Bytes
		}
		return prog.ParseWith("main", input, ParseOptions{Unit: unit, Recognize: recognize})
	}
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
