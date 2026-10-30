package gogen_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestLiteralTextPredicateDispatchRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testLiteralTextPredicateDispatchRejectsUnsafeShapes)
}

func testLiteralTextPredicateDispatchRejectsUnsafeShapes(t *testing.T) {
	cases := []string{
		`rule main = w:[a-z]+ #atomic {text($w) == "a" || len($w) == 1} $$`,
		`rule main = x:[a-z] #atomic y:[a-z] #atomic {text($x) == "a" || text($y) == "b"} $$`,
		`rule main = w:[a-z]+ #atomic {text($w) == "a" || !(text($w) == "b")} $$`,
		`var alias string
rule main = w:[a-z]+ #atomic {alias := text($w)} {alias == "a" || alias == "b"} $$`,
		`var result bool
rule main = w:[a-z]+ #atomic {result := text($w) == "a" || text($w) == "b"} $$`,
	}
	for i, source := range cases {
		g, err := parseTest(source)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		code, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "dispatch", Start: "main"})
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if strings.Contains(string(code), `switch c.TextStr(`) {
			t.Errorf("unsafe case %d was specialized", i)
		}
	}
}

func TestLiteralTextPredicateDispatchDisabledParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testLiteralTextPredicateDispatchDisabledParity)
}

func testLiteralTextPredicateDispatchDisabledParity(t *testing.T) {
	var cases []gentest.Case
	for _, c := range gentest.GenCorpus(t) {
		if strings.HasPrefix(c.Name, "literal text ") {
			cases = append(cases, c)
		}
	}
	if len(cases) < 5 {
		t.Fatalf("only %d literal text predicate cases", len(cases))
	}
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, gen.GenOptions{DisableLiteralTextDispatch: true})
}
