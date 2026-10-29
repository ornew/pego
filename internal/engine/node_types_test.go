package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// node[T] takes any value type T, and tuple types (A, B) are value types: they are checked
// statically, and at run time where a value may not fit, the same way on every backend and in
// generated parsers.
func nodeTypeCases() []gentest.Case {
	return []gentest.Case{
		{Name: "tuple node field", Src: "type P = { V node[(node[string], node[string])] }\nrule main P = v:(\"a\" \"b\") $$ => P{V: $v}", Inputs: []string{"ab", "a"}},
		{Name: "list node field", Src: "type P = { V node[[]node[string]] }\nrule main P = v:[a-z]* $$ => P{V: $v}", Inputs: []string{"ab", "", "1"}},
		{Name: "nested node field", Src: "type P = { V node[(node[string], node[(node[string], node[string])])] }\nrule main P = v:(\"a\" bc) $$ => P{V: $v}\nrule bc = \"b\" \"c\"", Inputs: []string{"abc", "ab"}},
		{Name: "tuple value field", Src: "type P = { T (node[string], node[string]) }\nrule main P = v:(\"a\" \"b\") $$ => P{T: $v.Value}", Inputs: []string{"ab"}},
		{Name: "optional tuple field", Src: "type P = { T *(node[string], node[string]) }\nrule main P = ab / c\nrule ab P = v:(\"a\" \"b\") $$ => P{T: $v.Value}\nrule c P = \"c\" $$ => P{T: nil}", Inputs: []string{"ab", "c", "a"}},
		{Name: "optional tuple list field", Src: "type P = { T []*(node[string], node[string]) }\nrule main P = v:(\"a\" \"b\") $$ => P{T: []*(node[string], node[string]){$v.Value, nil}}", Inputs: []string{"ab", "a"}},
		{Name: "union field", Src: "type P = { V node[(node[string], node[string])] | node[[]node[string]] }\nrule main P = v:(\"a\" \"b\" / \"c\"*) $$ => P{V: $v}", Inputs: []string{"ab", "cc", ""}},
		{Name: "rule type", Src: "rule main node[(node[string], node[[]node[string]])] = \"a\" \"b\"* $$", Inputs: []string{"abb", "a", "b"}},
		{Name: "cast tuple node", Src: "rule main = x:(\"a\" \"b\" / \"c\"*) $$ => $x.(node[_]).(node[(node[string], node[string])])", Inputs: []string{"ab", "cc", ""}},
		{Name: "cast list node", Src: "rule main = x:(\"a\" \"b\" / \"c\"*) $$ => $x.(node[_]).(node[[]node[string]])", Inputs: []string{"ab", "cc", ""}},
		{Name: "node of int", Src: "type P = { V *node[int] }\nrule main P = \"a\" $$ => P{V: nil}", Inputs: []string{"a"}},
	}
}

func TestNodeTypesAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range nodeTypeCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					for _, input := range fixture.Inputs {
						baseline := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: recognize}))
						for _, backend := range []Backend{Bytecode, BytecodeIterative} {
							actual := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}))
							if actual != baseline {
								t.Fatalf("%s/%s recognize=%t input=%q: %s / %s", backend, unit, recognize, input, actual, baseline)
							}
						}
					}
				}
			}
		})
	}
}

// A cast to node[T] checks the value of the node at run time.
func TestNodeTypeCasts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ typ, input string }{
		{"node[(node[string], node[string])]", "cc"},
		{"node[[]node[string]]", "ab"},
		{"node[(node[string], node[int])]", "ab"},
	} {
		program := mustCompile(t, "rule main = x:(\"a\" \"b\" / \"c\"*) $$ => $x.(node[_]).("+tc.typ+")")
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if _, err := program.ParseWith("main", tc.input, ParseOptions{Backend: backend}); err == nil || !strings.Contains(err.Error(), tc.typ) {
				t.Errorf("%s %s %q: got %v", tc.typ, backend, tc.input, err)
			}
		}
	}
}

// The declared node[T] and tuple types of fields and rules are checked when the grammar is
// compiled.
func TestNodeTypesCheckedAtCompileTime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, static, input string }{
		{"type P = { V node[[]node[string]] }\nrule main P = v:(\"a\" \"b\") => P{V: $v}", "cannot use", "ab"},
		{"type P = { V node[(node[string], node[string], node[string])] }\nrule main P = v:(\"a\" \"b\") => P{V: $v}", "cannot use", "ab"},
		{"type P = { T (node[string],) }\nrule main P = v:(\"a\" \"b\") => P{T: $v.Value}", "cannot use", "ab"},
		{"rule main node[[]node[string]] = \"a\" \"b\"", "not assignable", "ab"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.static) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.static)
		}
	}
}

func TestNodeTypesGeneratedGoParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testNodeTypesGeneratedGoParity)
}

func testNodeTypesGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	gentest.GeneratedParsersCorpus(t, goBin, nodeTypeCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, nodeTypeCases(), gen.GenOptions{})
	gentest.GeneratedTypesCorpus(t, goBin, nodeTypeCases(), gen.GenOptions{ConvertTypes: true})
}

func TestNodeTypesGeneratedTypeScriptParity(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, nodeTypeCases(), gen.GenOptions{})
}
