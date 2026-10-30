package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
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

// A module whose rule converts its value to a type that is not a named node type is rejected.
func TestNodeTypeModuleValidation(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, `type Name = node[string]
rule main = name ("," name)*
rule name Name = ([a-z]+) #atomic`, compile.Options{})
	m := *prog.Module()
	if err := compile.ValidateModule(&m); err != nil {
		t.Fatal(err)
	}
	m.Rules = append([]bytecode.RuleInfo(nil), m.Rules...)
	found := false
	for i := range m.Rules {
		if m.Rules[i].NodeType >= 0 {
			m.Rules[i].NodeType = m.Rules[i].Name
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no rule of a named node type")
	}
	if err := compile.ValidateModule(&m); err == nil || !strings.Contains(err.Error(), "is not a named node type") {
		t.Fatalf("got %v", err)
	}
}
