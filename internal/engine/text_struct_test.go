package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// textStructGrammar has rules that make a struct from the text they match, in the forms that
// textStructRule recognizes and in forms it must leave to the action.
const textStructGrammar = `type Name = { Text string }
type Num = { Text string }
type Pair = { Text string; N int }
type Other = { Text string }
type Out = { Items node }
rule main = items:(-" "* item)* $$ => Out{Items: $items}
rule item = tag / name / num / pair / plain / spaced / other
rule tag = "[" a:name "]" b:name {$b.Text == $a.Text} => $b
rule name Name = t:([a-z]+) #atomic => Name{Text: text($t)}
rule num = t:([0-9]+ ("." [0-9]+)?) #atomic => Num{Text: text($t)}
rule pair = "<" t:([a-z]+ #atomic) ">" => Pair{Text: text($t)}
rule plain = t:"!" => Other{Text: text($t)}
rule spaced Name = t:("#" [a-z]*) #atomic => Name{Text: text($t) + "#"}
rule other = t:("@" [a-z]*) #atomic => Other{Text: text($t)}`

func TestTextStructRules(t *testing.T) {
	prog := compile(t, `type Name = { Text string }
type Pair = { Text string; N int }
type Wide = { Text node[string] }
type Alias ~ Name
rule a Name = t:([a-z]+) #atomic => Name{Text: text($t)}
rule b = t:("x" #atomic) => Pair{Text: text($t)}
rule c = t:"x" => Name{Text: text($t)}
rule d = t:("x") #atomic => Name{Text: text($t) + ""}
rule e = t:("x") #atomic => Wide{Text: $t}
rule f Alias = t:("x") #atomic => Name{Text: text($t)}
rule g = u:("x") #atomic => Name{Text: text($u)}
rule h = t:("x") #atomic {true} => Name{Text: text($t)}
rule main = a / b / c / d / e / f / g / h`, Options{NoTypeCheck: true})
	want := map[string]bool{"a": true, "b": true, "f": true, "g": true}
	for _, r := range prog.rules {
		if r.textStruct != want[r.name] {
			t.Errorf("rule %s: textStruct %t, want %t", r.name, r.textStruct, want[r.name])
		}
	}
}

func TestTextStructsAcrossBackends(t *testing.T) {
	inputs := []string{"", "ab", "ab 12 3.5", "<x> ! #a # @b @", "ab ?", "12.", "<x", "! ab <yz> 7", "[ab]ab", "[ab]cd 1"}
	for _, sourceInfo := range []bool{false, true} {
		ref := compileBare(t, textStructGrammar, Options{SourceInfo: sourceInfo, NoTypeCheck: true, noTextStructs: true})
		prog := compileBare(t, textStructGrammar, Options{SourceInfo: sourceInfo, NoTypeCheck: true})
		fast := 0
		for _, r := range prog.rules {
			if r.textStruct {
				fast++
			}
		}
		if fast != 3 {
			t.Fatalf("%d text struct rules, want 3 (name, num, other)", fast)
		}
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, input := range inputs {
				want := resultJSON(ref.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit}))
				wantRecognized := resultJSON(ref.ParseWith("main", input, ParseOptions{Backend: Closure, Unit: unit, Recognize: true}))
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
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
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	cases := []genCase{{"text structs", textStructGrammar, []string{"", "ab", "ab 12 3.5", "<x> ! #a # @b @", "ab ?", "12.", "! ab <yz> 7", "[ab]ab", "[ab]cd 1"}}}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedParsersCorpus(t, goBin, cases, GenOptions{})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true}, 1)
	if node, err := exec.LookPath("node"); err == nil {
		testGeneratedTSParsersCorpus(t, node, cases, GenOptions{})
	}
}
