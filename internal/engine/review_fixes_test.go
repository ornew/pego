package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
)

// A fold whose accumulator keeps growing in nesting depth does not converge; the callback result
// must be cast.
func TestFoldAccumulatorMustConverge(t *testing.T) {
	t.Parallel()
	const prelude = "type T = { F [][][][]int }\n"
	for _, fold := range []string{"foldl", "foldr"} {
		src := prelude + `rule main T = d:"a"* $$ => T{F: ` + fold + `(list(), $d, (acc, e) => list(acc))}`
		if got := compileMessage(t, src); !strings.Contains(got, fold+": the accumulator type does not converge") {
			t.Errorf("%s: got %q", fold, got)
		}
	}
	// With a cast the callback result has the accumulator's type, and every backend agrees.
	program := mustCompile(t, prelude+`rule main T = d:"a"* $$ => T{F: foldl(list().([][][][]int), $d, (acc, e) => acc)}`)
	checkBackends(t, program, "main", "aaa")
	// A fold that converges within the widening is accepted.
	checkBackends(t, mustCompile(t, `rule main = d:"a"* $$ => foldl(0, $d, (acc, e) => acc + 1)`), "main", "aaa")
}

// Compiled bytes depend on the grammar, not on its layout, unless source information is recorded.
func TestCompiledBytesIgnoreLayoutWithoutSourceInfo(t *testing.T) {
	t.Parallel()
	const a = "rule main = x:(n / w) $$ => $x.(Num)\n" + gentest.CastRules
	b := "\n\n  " + strings.ReplaceAll(a, " => ", "   =>   ")
	bytesOf := func(src string, opts ...compile.Options) []byte {
		t.Helper()
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		o := compile.Options{}
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
	if bytes.Equal(bytesOf(a, compile.Options{SourceInfo: true}), bytesOf(b, compile.Options{SourceInfo: true})) {
		t.Error("compiled bytes with source information lack the cast position")
	}
}

// A node[_] is not a list for the list built-ins; it is cast first.
func TestNodeTopIsCastToSupplyElements(t *testing.T) {
	t.Parallel()
	const rules = "rule xs node[_] = \"a\"*\n"
	for _, fn := range []string{"map($n, (x) => x)", "concat($n, $n)", "filter($n, (x) => true)"} {
		if got := compileMessage(t, "rule main = n:xs $$ => "+fn+"\n"+rules); got == "" {
			t.Errorf("%s was accepted", fn)
		}
	}
	program := mustCompile(t, "rule main = n:xs $$ => len(map($n.([]_), (x) => x))\n"+rules)
	checkBackends(t, program, "main", "aaa")
	if got := gentest.ResultJSON(program.ParseWith("main", "aaa", ParseOptions{})); !strings.Contains(got, "3") {
		t.Errorf("got %s, want 3", got)
	}
}
