package gogen_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/compiletest"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/syntax"
	"github.com/ornew/pego/internal/value"
)

// The tests of this package compile the grammars that they generate parsers for with the engine,
// as package pego does.
func init() {
	gentest.Compile = compileForGenerator
	gentest.Reference = referenceProgram
}

// mustCompile compiles PEGO source with source information, so that results show rule names and
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

// compileBare compiles PEGO source with the options as given: without source information unless
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

// runtimeSource is the text of the runtime package, for tests that check its source.
var runtimeSource = func() string {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "runtime", "*.go"))
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

func parseTest(src string) (*grammar.Grammar, error) { return compiletest.Parse(src) }

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

func TestMain(m *testing.M) {
	// Tests build generated programs in fresh temporary directories. Without -trimpath their
	// build cache entries depend on the directory and are never reused, so every run would add
	// tens of gigabytes of builds to the cache. Commands that set GOFLAGS add it themselves.
	// -mod=mod lets the temporary modules that replace this module resolve it without a go.sum.
	os.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -trimpath -mod=mod"))
	code := m.Run()
	if names := gentest.UnwrappedFixtureTests(); len(names) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: tests build generated fixtures without choosing their form: %s\nWrap the test body in gentest.EachMode, or call gentest.NeedsBudgetControls, or pin the form with gentest.PinOrdinary\n", strings.Join(names, ", "))
		code = 1
	}
	os.Exit(code)
}

// compileMessage compiles src and returns the error message, or "" if it compiles.
func compileMessage(t *testing.T, src string, opts ...compile.Options) string {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	var o compile.Options
	if len(opts) > 0 {
		o = opts[0]
	}
	_, err = engine.Compile(g, o)
	if err == nil {
		return ""
	}
	return err.Error()
}

func resultWith(prog *engine.Program, input string, o engine.ParseOptions) string {
	n, err := prog.ParseWith("main", input, o)
	if err != nil {
		return "error: " + err.Error()
	}
	return n.String()
}

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

// startEntryWant is what the interpreter reports for each of gentest.StartEntryNames, one line each.
func startEntryWant(t *testing.T) string {
	t.Helper()
	prog, err := compileStarts(t, gentest.StartRestrictionSource)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, name := range gentest.StartEntryNames {
		if err := prog.CheckStart(name); err != nil {
			fmt.Fprintf(&b, "%s: %v\n", name, err)
		} else {
			fmt.Fprintf(&b, "%s: ok\n", name)
		}
	}
	return b.String()
}

// ignoreCaseLines is the expected output of a program that parses each input and prints "ok" with
// the consumed input, or "error".
func ignoreCaseLines(t *testing.T, src string, inputs []string) string {
	t.Helper()
	prog := mustCompile(t, src)
	var b strings.Builder
	for _, in := range inputs {
		n, err := prog.ParseWith("main", in, engine.ParseOptions{Expectations: true, Backend: engine.Closure})
		if err != nil {
			b.WriteString("error\n")
			continue
		}
		r := []rune(in)
		fmt.Fprintf(&b, "ok %s\n", string(r[n.Start():n.End()]))
	}
	return b.String()
}

// firstCharEnd returns the position after the character at pos in the unit u: what a call that
// looks at that character only has examined, which is pos+1 at the end of the input.
func firstCharEnd(input string, pos int, u engine.PositionUnit) int {
	if u == engine.Bytes && pos < len(input) {
		_, size := utf8.DecodeRuneInString(input[pos:])
		return pos + size
	}
	return pos + 1
}
