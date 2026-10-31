package tsgen_test

import (
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/compiletest"
	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/syntax"
	"github.com/ornew/zeroparse/internal/value"
)

// The tests of this package compile the grammars that they generate parsers for with the engine,
// as package zeroparse does.
func init() {
	gentest.Compile = compileForGenerator
	gentest.Reference = referenceProgram
}

// mustCompile compiles ZeroParse source with source information, so that results show rule names and
// capture labels.
func mustCompile(t *testing.T, src string, opts ...compile.Options) *engine.Program {
	t.Helper()
	var o compile.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	o.SourceInfo = true
	return compileBare(t, src, o)
}

// compileBare compiles ZeroParse source with the options as given: without source information unless
// they request it.
func compileBare(t *testing.T, src string, opts ...compile.Options) *engine.Program {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	var o compile.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	prog, err := engine.Compile(g, o)
	if err != nil {
		t.Fatalf("compile error: %v\n%s", err, src)
	}
	return prog
}

// referenceProgram compiles src with the engine for the comparisons of generated parsers with it.
func referenceProgram(t *testing.T, src string, opts compile.Options) gentest.ReferenceParse {
	t.Helper()
	prog := compileBare(t, src, opts)
	return func(input string, bytes, recognize bool) (*value.Node, error) {
		unit := engine.CodePoints
		if bytes {
			unit = engine.Bytes
		}
		return prog.ParseWith("main", input, engine.ParseOptions{Unit: unit, Recognize: recognize, Expectations: true})
	}
}

// compileForGenerator compiles g for the generators, as the root package's compileForGenerator
// does.
func compileForGenerator(g *grammar.Grammar, opts compile.Options) (*compile.Program, func() (*compile.Program, error), error) {
	prog, err := engine.Compile(g, opts)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*compile.Program, error) {
		rec, err := prog.Recognizer()
		if err != nil {
			return nil, err
		}
		return rec.Static(), nil
	}
	return prog.Static(), recognizer, nil
}

func parseTest(src string) (*grammar.Grammar, error) { return compiletest.Parse(src) }

func mustParse(src string) *grammar.Grammar {
	g, err := parseTest(src)
	if err != nil {
		panic(err)
	}
	return g
}

// compileStarts compiles source as written, without marking start rules for the test.
func compileStarts(t *testing.T, src string) (*engine.Program, error) {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	return engine.Compile(g, compile.Options{})
}

// inferredType returns the value type the checker infers for the rule name, with the rules it
// refers to replaced by their values at the top level and in unions and optional types. RuleType
// refers to a rule without a declared type by name instead.
func inferredType(p *engine.Program, name string) (string, bool) {
	t, ok := p.Typed.Values[name]
	if !ok {
		return "", false
	}
	v := &compile.ValueInference{Prog: p.Static(), Out: p.Typed.Values}
	return v.Expand(t).String(), true
}
