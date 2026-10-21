package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// A fold whose accumulator keeps growing in nesting depth does not converge; the callback result
// must be cast.
func TestFoldAccumulatorMustConverge(t *testing.T) {
	const prelude = "type T = { F [][][][]int }\n"
	for _, fold := range []string{"foldl", "foldr"} {
		src := prelude + `rule main T = d:"a"* $$ => T{F: ` + fold + `(list(), $d, (acc, e) => list(acc))}`
		if got := compileMessage(t, src); !strings.Contains(got, fold+": the accumulator type does not converge") {
			t.Errorf("%s: got %q", fold, got)
		}
	}
	// With a cast the callback result has the accumulator's type, and every backend agrees.
	program := compile(t, prelude+`rule main T = d:"a"* $$ => T{F: foldl(list().([][][][]int), $d, (acc, e) => acc)}`)
	checkBackends(t, program, "main", "aaa")
	// A fold that converges within the widening is accepted.
	checkBackends(t, compile(t, `rule main = d:"a"* $$ => foldl(0, $d, (acc, e) => acc + 1)`), "main", "aaa")
}

// Compiled bytes depend on the grammar, not on its layout, unless source information is recorded.
func TestCompiledBytesIgnoreLayoutWithoutSourceInfo(t *testing.T) {
	const a = "rule main = x:(n / w) $$ => $x.(Num)\n" + castRules
	b := "\n\n  " + strings.ReplaceAll(a, " => ", "   =>   ")
	bytesOf := func(src string, opts ...Options) []byte {
		t.Helper()
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		o := Options{}
		if len(opts) > 0 {
			o = opts[0]
		}
		p, err := Compile(g, o)
		if err != nil {
			t.Fatal(err)
		}
		data, err := p.MarshalBinary("main")
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if !bytes.Equal(bytesOf(a), bytesOf(b)) {
		t.Error("compiled bytes depend on whitespace without source information")
	}
	if bytes.Equal(bytesOf(a, Options{SourceInfo: true}), bytesOf(b, Options{SourceInfo: true})) {
		t.Error("compiled bytes with source information lack the cast position")
	}
}

// A variable declared with the retired name of the top type is told to use _.
func TestVarOfRetiredTypeName(t *testing.T) {
	if got := compileMessage(t, "var d any\nrule main = \"a\""); !strings.Contains(got, "any is not a type; write _") {
		t.Errorf("got %q", got)
	}
}

// The type of a recursive rule nests without end; a value of it that does not fit is a mismatch,
// not a value of unknown type, so it gets no cast hint. A value of type _ does.
func TestCastHintOnlyForUnknownTypes(t *testing.T) {
	const recursive = "type T = { F [][]int }\nrule n = p / q\nrule p = \"(\" x:n \")\" => list($x)\nrule q = \"a\" => list()\nrule main T = v:n $$ => T{F: $v}"
	got := compileMessage(t, recursive)
	if !strings.Contains(got, "cannot use") || strings.Contains(got, "not known") {
		t.Errorf("recursive: %q", got)
	}
	got = compileMessage(t, "type T = { F int }\nrule main T = d:one $$ => T{F: $d}\nrule one _ = _ => 1")
	if !strings.Contains(got, "the type of the value is not known") {
		t.Errorf("unknown: %q", got)
	}
}

// A node[_] is not a list for the list built-ins; it is cast first.
func TestNodeTopIsCastToSupplyElements(t *testing.T) {
	const rules = "rule xs node[_] = \"a\"*\n"
	for _, fn := range []string{"map($n, (x) => x)", "concat($n, $n)", "filter($n, (x) => true)"} {
		if got := compileMessage(t, "rule main = n:xs $$ => "+fn+"\n"+rules); got == "" {
			t.Errorf("%s was accepted", fn)
		}
	}
	program := compile(t, "rule main = n:xs $$ => len(map($n.([]_), (x) => x))\n"+rules)
	checkBackends(t, program, "main", "aaa")
	if got := resultJSON(program.ParseWith("main", "aaa", ParseOptions{})); !strings.Contains(got, "3") {
		t.Errorf("got %s, want 3", got)
	}
}
