package engine

import (
	"github.com/ornew/pego/internal/gentest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

// valueResultCases have rules and list elements whose values are ints, strings and bools.
func valueResultCases() []gentest.Case {
	return []gentest.Case{
		{Name: "int rule", Src: "rule main = d:[0-9]+ #atomic $$ => len($d)", Inputs: []string{"123", "7"}},
		{Name: "bool rule", Src: "rule main bool = d:[0-9]* #atomic $$ => len($d) > 1", Inputs: []string{"123", "1"}},
		{Name: "map text", Src: "rule main = xs:([a-z] #atomic)+ $$ => map($xs.Value, (x) => text(x))", Inputs: []string{"abc", "a"}},
		{Name: "list scalars", Src: "rule main = \"a\" $$ => list(1, \"s\", true, nil)", Inputs: []string{"a"}},
		{Name: "typed list", Src: "rule main = \"a\" $$ => []int{1, 2}", Inputs: []string{"a"}},
		{Name: "int elements", Src: "rule n int = d:[0-9]+ #atomic => len($d)\nrule main = xs:(n -\",\"?)+ $$ => map($xs.Value, (x) => x[0] + 1)", Inputs: []string{"1,22,333"}},
		{Name: "string field", Src: "type T = { A []string }\nrule main = xs:([a-z] #atomic)+ $$ => T{A: map($xs.Value, (x) => text(x))}", Inputs: []string{"ab"}},
		{Name: "fold ints", Src: "rule main = xs:([0-9]+ #atomic -\" \"?)+ $$ => foldl(0, map($xs.Value, (x) => len(x[0])), (a, b) => a + b)", Inputs: []string{"1 22 333"}},
		{Name: "filter and index", Src: "rule main = \"a\" $$ => filter(list(1, 2, 3), (x) => x > 1)[1]", Inputs: []string{"a"}},
		{Name: "concat", Src: "rule main = \"a\" $$ => concat(list(\"x\"), []string{\"y\"}, map(list(1, 2), (x) => x * 2))", Inputs: []string{"a"}},
	}
}

func TestValueResultsAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range valueResultCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, input := range fixture.Inputs {
				baseline := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure}))
				if strings.Contains(baseline, "error") {
					t.Fatalf("input=%q: %s", input, baseline)
				}
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					if actual := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend})); actual != baseline {
						t.Fatalf("%s input=%q: %s / %s", backend, input, actual, baseline)
					}
				}
			}
		})
	}
}

func TestValueResults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, input, want string }{
		{"rule main = d:[0-9]+ #atomic $$ => len($d)", "123", "3"},
		{"rule main = xs:([a-z] #atomic)+ $$ => map($xs.Value, (x) => text(x))", "ab", `["a" "b"]`},
		{"rule main = \"a\" $$ => list(1, \"s\", true, nil)", "a", `[1 "s" true nil]`},
		{"rule main = \"a\" $$ => filter(list(1, 2, 3), (x) => x > 1)[1]", "a", "3"},
	} {
		n, err := mustCompile(t, tc.src).Parse("main", tc.input)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if got := n.String(); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.src, got, tc.want)
		}
	}
	// A function is still not a value.
	for _, src := range []string{
		"rule main = \"a\" => list((x) => x)",
		"rule main = \"a\" => (x) => x",
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Errorf("%q: accepted", src)
		}
	}
}

func TestValueResultsGeneratedGoParity(t *testing.T) {
	t.Parallel()
	eachMode(t, testValueResultsGeneratedGoParity)
}

func testValueResultsGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, valueResultCases(), gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, valueResultCases(), gen.GenOptions{ConvertTypes: true})
}

func TestValueResultsGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, valueResultCases(), gen.GenOptions{})
}
