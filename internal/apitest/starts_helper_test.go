package apitest_test

import (
	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/starttest"
)

// compileTestSource compiles ZeroParse source as zp.CompileSource does, for a test whose subject is
// not start rules: unless the source has declaration attributes, every rule becomes a start rule,
// the first one the default.
func compileTestSource(src, start string, opts ...zp.CompileOption) (*zp.Parser, error) {
	g, err := zp.ParseGrammar(src)
	if err != nil {
		return nil, err
	}
	return zp.Compile(starttest.MarkAll(g), start, opts...)
}
