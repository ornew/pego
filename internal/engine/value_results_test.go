package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
)

// valueResultCases have rules and list elements whose values are ints, strings and bools.
func valueResultCases() []genCase {
	return []genCase{
		{"int rule", "rule main = d:[0-9]+ #atomic $$ => len($d)", []string{"123", "7"}},
		{"bool rule", "rule main bool = d:[0-9]* #atomic $$ => len($d) > 1", []string{"123", "1"}},
		{"map text", "rule main = xs:([a-z] #atomic)+ $$ => map($xs.Value, (x) => text(x))", []string{"abc", "a"}},
		{"list scalars", "rule main = \"a\" $$ => list(1, \"s\", true, nil)", []string{"a"}},
		{"typed list", "rule main = \"a\" $$ => []int{1, 2}", []string{"a"}},
		{"int elements", "rule n int = d:[0-9]+ #atomic => len($d)\nrule main = xs:(n -\",\"?)+ $$ => map($xs.Value, (x) => x[0] + 1)", []string{"1,22,333"}},
		{"string field", "type T = { A []string }\nrule main = xs:([a-z] #atomic)+ $$ => T{A: map($xs.Value, (x) => text(x))}", []string{"ab"}},
		{"fold ints", "rule main = xs:([0-9]+ #atomic -\" \"?)+ $$ => foldl(0, map($xs.Value, (x) => len(x[0])), (a, b) => a + b)", []string{"1 22 333"}},
		{"filter and index", "rule main = \"a\" $$ => filter(list(1, 2, 3), (x) => x > 1)[1]", []string{"a"}},
		{"concat", "rule main = \"a\" $$ => concat(list(\"x\"), []string{\"y\"}, map(list(1, 2), (x) => x * 2))", []string{"a"}},
	}
}

func TestValueResultsAcrossBackends(t *testing.T) {
	for _, fixture := range valueResultCases() {
		t.Run(fixture.name, func(t *testing.T) {
			program := mustCompile(t, fixture.src)
			for _, input := range fixture.inputs {
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

func TestValueResultsGeneratedGoParity(t *testing.T) { eachMode(t, testValueResultsGeneratedGoParity) }

func testValueResultsGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, valueResultCases(), gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, valueResultCases(), gen.GenOptions{ConvertTypes: true})
}

func TestValueResultsGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, valueResultCases(), gen.GenOptions{})
}
