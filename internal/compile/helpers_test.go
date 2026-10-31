package compile

import (
	"context"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/syntax"
)

// The helpers of the tests that check the compile phase: they compile ZeroParse source with
// compileStatic, which builds no matchers. The engine's tests keep their own copies, which return
// the engine's program.

// mustParse parses ZeroParse source with parseTest, panicking on a syntax error.
func mustParse(src string) *grammar.Grammar {
	g, err := parseTest(src)
	if err != nil {
		panic(err)
	}
	return g
}

// mustCompile compiles ZeroParse source with source information, so that results show rule names and
// capture labels.
func mustCompile(t *testing.T, src string, opts ...Options) *Program {
	t.Helper()
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	o.SourceInfo = true
	return compileBare(t, src, o)
}

// compileBare compiles ZeroParse source with the options as given: without source information unless
// they request it.
func compileBare(t *testing.T, src string, opts ...Options) *Program {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	prog, err := compileStatic(g, o, nil)
	if err != nil {
		t.Fatalf("compile error: %v\n%s", err, src)
	}
	return prog
}

// compileError returns the compile error message.
func compileError(t *testing.T, src string) string {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	_, err = compileStatic(g, Options{}, nil)
	if err == nil {
		t.Fatalf("expected a compile error\n%s", src)
	}
	return err.Error()
}

// compileMessage compiles src and returns the error message, or "" if it compiles.
func compileMessage(t *testing.T, src string, opts ...Options) string {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	_, err = compileStatic(g, o, nil)
	if err == nil {
		return ""
	}
	return err.Error()
}

// compileStarts parses src as written, without marking start rules, and compiles it.
func compileStarts(t *testing.T, src string) (*Program, error) {
	t.Helper()
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatalf("syntax error: %v\n%s", err, src)
	}
	return compileStatic(g, Options{}, nil)
}

// inferredType returns the value type the checker infers for the rule name, with the rules it
// refers to replaced by their values at the top level and in unions and optional types. RuleType
// refers to a rule without a declared type by name instead.
func inferredType(p *Program, name string) (string, bool) {
	t, ok := p.Typed.Values[name]
	if !ok {
		return "", false
	}
	v := &ValueInference{Prog: p, Out: p.Typed.Values}
	return v.Expand(t).String(), true
}

// contextClassRanges returns 4096 single-code-point ranges, enough for the class compiler to poll.
func contextClassRanges() []grammar.CharRange {
	rs := make([]grammar.CharRange, 4096)
	for i := range rs {
		rs[i] = grammar.CharRange{Lo: rune(256 + i*2), Hi: rune(256 + i*2)}
	}
	return rs
}

// jsonActionBlockGrammar returns g after a round trip through its JSON form.
func jsonActionBlockGrammar(t *testing.T, g *grammar.Grammar) *grammar.Grammar {
	t.Helper()
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	result, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// varModuleSource declares variables of each type, reads them in predicates and rebinds them.
const varModuleSource = `var n int = 3
var s string = "x"
var b bool
rule main = {n := n + 1} {s := s + "y"} {n > 2 && s != "" && !b} item "a"
rule item = {n > 0} "b"?
`

// compileContext is compileStatic under ctx, as the engine's CompileContext is: cancellation
// discards the program and returns the context's error.
func compileContext(ctx context.Context, g *grammar.Grammar, opts Options) (prog *Program, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(Cancellation); ok {
				prog, err = nil, cancelled.Err
			} else {
				panic(failure)
			}
		}
	}()
	names := InlineNames{}
	prog, err = CompileProgram(g, opts, nil, names, &Control{Ctx: ctx}, func(*Compiler) ExprBuilder[struct{}] { return noExprs{} })
	if err != nil {
		return nil, names.Rewrite(err)
	}
	return prog, nil
}
