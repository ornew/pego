package engine

import (
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
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
