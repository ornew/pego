package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// textStructGrammar has rules whose actions make structs with a field Text from the text they
// match.
const textStructGrammar = `type Name = { Text string }
type Num = { Text string }
type Pair = { Text string; N int }
type Other = { Text string }
type Out = { Items node[_] }
rule main = items:(-" "* item)* $$ => Out{Items: $items}
rule item = tag / name / num / pair / plain / spaced / other
rule tag = "[" a:name "]" b:name {$b.Text == $a.Text} => $b
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}
rule num = t:([0-9]+ ("." [0-9]+)?) #atomic => Num{Text: text($t)}
rule pair = "<" t:([a-z]+ #atomic) ">" => Pair{Text: text($t)}
rule plain = t:"!" => Other{Text: text($t)}
rule spaced Name = t:("#" [a-z]*) #atomic => Name{Text: text($t) + "#"}
rule other = t:("@" [a-z]*) #atomic => Other{Text: text($t)}`

// TestTextStructsAcrossBackends checks that the backends agree on the structs actions make from
// the text a rule matches.
func TestTextStructsAcrossBackends(t *testing.T) {
	t.Parallel()
	inputs := []string{"", "ab", "ab 12 3.5", "<x> ! #a # @b @", "ab ?", "12.", "<x", "! ab <yz> 7", "[ab]ab", "[ab]cd 1"}
	for _, sourceInfo := range []bool{false, true} {
		prog := compileBare(t, textStructGrammar, compile.Options{SourceInfo: sourceInfo})
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range inputs {
				want := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit}))
				wantRecognized := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: true}))
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					got := resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit}))
					if got != want {
						t.Errorf("sourceInfo=%t %s/%s %q:\n got %s\nwant %s", sourceInfo, backend, unit, input, got, want)
					}
					got = resultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend, Unit: unit, Recognize: true}))
					if got != wantRecognized {
						t.Errorf("recognize sourceInfo=%t %s/%s %q:\n got %s\nwant %s", sourceInfo, backend, unit, input, got, wantRecognized)
					}
				}
			}
		}
	}
}

func TestGeneratedTextStructs(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTextStructs)
}

func testGeneratedTextStructs(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := []gentest.Case{{Name: "text structs", Src: textStructGrammar, Inputs: []string{"", "ab", "ab 12 3.5", "<x> ! #a # @b @", "ab ?", "12.", "! ab <yz> 7", "[ab]ab", "[ab]cd 1"}}}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, 1)
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, gen.GenOptions{})
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
