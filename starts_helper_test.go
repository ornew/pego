package pego_test

import (
	"github.com/ornew/pego"
	"github.com/ornew/pego/internal/starttest"
)

// compileTestSource compiles PEGO source as pego.CompileSource does, for a test whose subject is
// not start rules: unless the source has declaration attributes, every rule becomes a start rule,
// the first one the default.
func compileTestSource(src, start string, opts ...pego.CompileOption) (*pego.Parser, error) {
	g, err := pego.ParseGrammar(src)
	if err != nil {
		return nil, err
	}
	return pego.Compile(starttest.MarkAll(g), start, opts...)
}
