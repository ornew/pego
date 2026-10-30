package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
)

// TestTextStructsAcrossBackends checks that the backends agree on the structs actions make from
// the text a rule matches.
func TestTextStructsAcrossBackends(t *testing.T) {
	t.Parallel()
	inputs := []string{"", "ab", "ab 12 3.5", "<x> ! #a # @b @", "ab ?", "12.", "<x", "! ab <yz> 7", "[ab]ab", "[ab]cd 1"}
	for _, sourceInfo := range []bool{false, true} {
		prog := compileBare(t, gentest.TextStructGrammar, compile.Options{SourceInfo: sourceInfo})
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range inputs {
				want := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit}))
				wantRecognized := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: true}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					got := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit}))
					if got != want {
						t.Errorf("sourceInfo=%t %s/%s %q:\n got %s\nwant %s", sourceInfo, backend, unit, input, got, want)
					}
					got = gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: true}))
					if got != wantRecognized {
						t.Errorf("recognize sourceInfo=%t %s/%s %q:\n got %s\nwant %s", sourceInfo, backend, unit, input, got, wantRecognized)
					}
				}
			}
		}
	}
}

