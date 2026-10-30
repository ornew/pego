package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gentest"
)

func TestNodeTypesAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.NodeTypeCases() {
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
