package engine

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// textStructGrammar has rules that make a struct from the text they match, in the forms that
// textStructRule recognizes and in forms it must leave to the action.
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
		if (r.textType != "") != want[r.name] {
			t.Errorf("rule %s: text type %q, want a text struct rule: %t", r.name, r.textType, want[r.name])
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
			if r.textType != "" {
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

// TestTextStructNodeViews checks that a text struct, which keeps its Text field unboxed, reads
// like the struct its action makes through every view of a tree: printing, JSON, fields, Walk,
// clones, snapshots, structural queries and Documents.
func TestTextStructNodeViews(t *testing.T) {
	ref := compileBare(t, textStructGrammar, Options{NoTypeCheck: true, noTextStructs: true})
	prog := compileBare(t, textStructGrammar, Options{NoTypeCheck: true})
	query, err := (*QuerySchema)(nil).CompileStructuralQuery(QueryPattern{Type: "Name", Fields: []QueryFieldPattern{{Name: "Text", Pattern: QueryPattern{Capture: "t"}}}})
	if err != nil {
		t.Fatal(err)
	}
	views := func(p *Program, input string) string {
		var b strings.Builder
		root, err := p.Parse("main", input)
		fmt.Fprintf(&b, "%s %v\n", root, err)
		if root == nil {
			return b.String()
		}
		js, _ := json.Marshal(root)
		fmt.Fprintf(&b, "json %s\nclone %s\n", js, root.Clone())
		Walk(root, func(v WalkVisit) (WalkAction, error) {
			fmt.Fprintf(&b, "walk %d %v %s\n", v.Depth, v.Edge, viewValue(v.Value))
			if n := v.Node; n != nil && n.Kind() == KindStruct {
				fmt.Fprintf(&b, "  Text=%s N=%s", viewValue(n.Field("Text")), viewValue(n.Field("N")))
				for name, value := range n.Fields() {
					fmt.Fprintf(&b, " %s:%s", name, viewValue(value))
				}
				b.WriteString("\n")
			}
			return WalkContinue, nil
		})
		matches, err := query.Matches(root, QueryOptions{})
		fmt.Fprintf(&b, "query %v", err)
		for _, m := range matches {
			for _, c := range m.Captures {
				fmt.Fprintf(&b, " %s=%s", c.Name, viewValue(c.Value))
			}
		}
		b.WriteString("\n")
		d, err := p.NewChangeDocumentWith("main", input, ParseOptions{}, 16)
		if err != nil {
			t.Fatal(err)
		}
		s, err := d.Snapshot()
		if err == nil {
			var walk func(n SnapshotNode, depth int)
			walk = func(n SnapshotNode, depth int) {
				fmt.Fprintf(&b, "snapshot %d %s %q", depth, n.Type(), n.Text())
				for _, f := range n.Fields() {
					if c, ok := f.Value.(SnapshotNode); ok {
						fmt.Fprintf(&b, " %s:", f.Name)
						walk(c, depth+1)
					} else {
						fmt.Fprintf(&b, " %s:%#v", f.Name, f.Value)
					}
				}
				for i := 0; i < n.ChildCount(); i++ {
					walk(n.Child(i), depth+1)
				}
				b.WriteString("\n")
			}
			walk(s.Root(), 0)
		}
		fmt.Fprintf(&b, "snapshot error %v\n", err)
		// An edit that reuses the unchanged items.
		if err := d.Edit(0, 0, "zz "); err == nil {
			again, err := d.Parse()
			fmt.Fprintf(&b, "edited %s %v\n", again, err)
			s2, err := d.Snapshot()
			if err == nil {
				fmt.Fprintf(&b, "changes %v\n", equalChangeSnapshots(s, s2))
			}
		}
		return b.String()
	}
	for _, input := range []string{"ab", "ab 12 3.5", "<x> ! #a # @b @", "[ab]ab", "[ab]cd 1", "ab ?"} {
		if got, want := views(prog, input), views(ref, input); got != want {
			t.Errorf("%q:\n got %s\nwant %s", input, got, want)
		}
	}
}

// viewValue prints a value of a tree, a node by its value.
func viewValue(v any) string {
	if n, ok := v.(*Node); ok {
		return fmt.Sprintf("%s[%d,%d)", n, n.Start(), n.End())
	}
	return fmt.Sprintf("%#v", v)
}

// A module whose rule names a text type that is not a declared type with a field Text is rejected.
func TestTextStructModuleValidation(t *testing.T) {
	prog := compileBare(t, textStructGrammar, Options{NoTypeCheck: true})
	m := *prog.Module()
	if err := validateModule(&m); err != nil {
		t.Fatal(err)
	}
	m.Rules = append([]RuleInfo(nil), m.Rules...)
	found := false
	for i := range m.Rules {
		if m.Rules[i].TextType >= 0 {
			m.Rules[i].TextType = m.Rules[i].Name
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no text struct rule")
	}
	if err := validateModule(&m); err == nil || !strings.Contains(err.Error(), "is not a declared type with a field Text") {
		t.Fatalf("got %v", err)
	}
}
