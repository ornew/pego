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

// node[T] takes any value type T, and tuple types (A, B) are value types: they are checked
// statically, and at run time where a value may not fit, the same way on every backend and in
// generated parsers.
func nodeTypeCases() []genCase {
	return []genCase{
		{"tuple node field", "type P = { V node[(node[string], node[string])] }\nrule main P = v:(\"a\" \"b\") $$ => P{V: $v}", []string{"ab", "a"}},
		{"list node field", "type P = { V node[[]node[string]] }\nrule main P = v:[a-z]* $$ => P{V: $v}", []string{"ab", "", "1"}},
		{"nested node field", "type P = { V node[(node[string], node[(node[string], node[string])])] }\nrule main P = v:(\"a\" bc) $$ => P{V: $v}\nrule bc = \"b\" \"c\"", []string{"abc", "ab"}},
		{"tuple value field", "type P = { T (node[string], node[string]) }\nrule main P = v:(\"a\" \"b\") $$ => P{T: $v.Value}", []string{"ab"}},
		{"optional tuple field", "type P = { T *(node[string], node[string]) }\nrule main P = ab / c\nrule ab P = v:(\"a\" \"b\") $$ => P{T: $v.Value}\nrule c P = \"c\" $$ => P{T: nil}", []string{"ab", "c", "a"}},
		{"optional tuple list field", "type P = { T []*(node[string], node[string]) }\nrule main P = v:(\"a\" \"b\") $$ => P{T: []*(node[string], node[string]){$v.Value, nil}}", []string{"ab", "a"}},
		{"union field", "type P = { V node[(node[string], node[string])] | node[[]node[string]] }\nrule main P = v:(\"a\" \"b\" / \"c\"*) $$ => P{V: $v}", []string{"ab", "cc", ""}},
		{"rule type", "rule main node[(node[string], node[[]node[string]])] = \"a\" \"b\"* $$", []string{"abb", "a", "b"}},
		{"cast tuple node", "rule main = x:(\"a\" \"b\" / \"c\"*) $$ => $x.(node[_]).(node[(node[string], node[string])])", []string{"ab", "cc", ""}},
		{"cast list node", "rule main = x:(\"a\" \"b\" / \"c\"*) $$ => $x.(node[_]).(node[[]node[string]])", []string{"ab", "cc", ""}},
		{"node of int", "type P = { V *node[int] }\nrule main P = \"a\" $$ => P{V: nil}", []string{"a"}},
	}
}

func TestNodeTypesAcrossBackends(t *testing.T) {
	for _, fixture := range nodeTypeCases() {
		t.Run(fixture.name, func(t *testing.T) {
			program := mustCompile(t, fixture.src)
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					for _, input := range fixture.inputs {
						baseline := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: recognize}))
						for _, backend := range []Backend{Bytecode, BytecodeIterative} {
							actual := resultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}))
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

func TestNodeTypesGeneratedGoParity(t *testing.T) { eachMode(t, testNodeTypesGeneratedGoParity) }

func testNodeTypesGeneratedGoParity(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, nodeTypeCases(), gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, nodeTypeCases(), gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, nodeTypeCases(), gen.GenOptions{ConvertTypes: true})
}

func TestNodeTypesGeneratedTypeScriptParity(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	testGeneratedTSParsersCorpus(t, node, nodeTypeCases(), gen.GenOptions{})
}
