package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/ir/core"
)

// coreCorpusProgram compiles a corpus grammar for the Core IR tests, or reports false for a
// grammar that does not compile or that generated parsers refuse (#missing).
func coreCorpusProgram(t *testing.T, c genCase) (*Program, bool) {
	t.Helper()
	g, err := parseTest(c.src)
	if err != nil {
		return nil, false
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil || prog.HasMissing {
		return nil, false
	}
	return prog, true
}

// TestCoreBuildCorpus builds, analyzes and verifies the Core IR of every grammar of the corpus
// and of the parsers directory, and checks that the scope of every rule is the frame layout the
// generated Node rules use, which the typed rules share.
func TestCoreBuildCorpus(t *testing.T) {
	built := 0
	for _, c := range schemaCorpus(t) {
		prog, ok := coreCorpusProgram(t, c)
		if !ok {
			continue
		}
		m, err := buildCore(prog.staticProgram)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		core.Analyze(m)
		if err := core.Verify(m); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		g, ok := coreGenerate(c.src, gen.GenOptions{})
		if !ok {
			continue
		}
		for _, r := range typedRules(g) {
			if _, ok := m.Rules[r.ID].Body.(*core.OperatorsRule); ok {
				continue
			}
			if got, want := core.SlotNames(m.Rules[r.ID].Scope), g.nodeScopes[r]; !slices.Equal(got, want) {
				t.Errorf("%s: rule %s (%d): scope %v, generated Node rule %v", c.name, r.Name, r.ID, got, want)
			}
		}
		built++
	}
	if built < 100 {
		t.Fatalf("built %d grammars", built)
	}
}

func TestCoreBuildShapes(t *testing.T) {
	src := `rule main = a:x b:("c" / d)* ("e" [0-9]) #atomic -"f" !"g" &(h:"h") $$ => $a
rule x = "x" "y"
rule d = "d"
`
	prog, ok := coreCorpusProgram(t, genCase{src: src})
	if !ok {
		t.Fatal("does not compile")
	}
	m, err := buildCore(prog.staticProgram)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Fatal(err)
	}
	got := core.Print(m, false)
	for _, want := range []string{
		"scope=[a b h] => $0:a",
		"  seq\n    capture 0 a\n      call 1:x\n",
		"    capture 1 b\n      repeat 0 -1\n",
		"    text\n      seq\n",
		"    drop\n      lit \"f\"\n",
		"    not\n      lit \"g\"\n",
		"    and\n      capture 2 h\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Print does not contain %q:\n%s", want, got)
		}
	}
	main := m.Rules[0]
	if main.Want || main.Finish.Kind != core.FinishAction {
		t.Errorf("main: want=%v finish=%v; want a lean rule with an action", main.Want, main.Finish.Kind)
	}
	// The capture builds the value of its body, a call of x itself rather than its twin.
	capture := main.Body.(*core.Seq).Items[0].(*core.Capture)
	if call := capture.Body.(*core.Call); !call.Attrs().Want || m.Rules[call.Rule].Policy.NoValue {
		t.Errorf("captured call: want=%v novalue=%v", call.Attrs().Want, m.Rules[call.Rule].Policy.NoValue)
	}
	if x := m.Rules[1]; !x.Want || x.Finish.Kind != core.FinishScope || !x.Finish.Shape.Tuple {
		t.Errorf("x: want=%v finish=%v shape=%+v", x.Want, x.Finish.Kind, x.Finish.Shape)
	}
}

// coreProgram compiles src, failing the test if it does not compile.
func coreProgram(t *testing.T, src string) *Program {
	t.Helper()
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return prog
}

// typeString returns the checker's spelling of a Core type handle.
func typeString(x core.Type) string {
	if x == nil {
		return "<nil>"
	}
	return x.(compile.Ty).String()
}

// TestCoreBuildTypes checks that the module carries the checked types: named types, rule values,
// capture slots of rules and of repetition elements, and every term.
func TestCoreBuildTypes(t *testing.T) {
	prog := coreProgram(t, `type P = { N int; S string }
var k int = 2
rule main P = p:pair xs:(-"," q:pair)* => P{N: $p.N + len($xs) * k, S: $p.S}
rule pair P = n:[0-9]+ s:[a-z] => P{N: len($n), S: text($s)}
`)
	m, err := buildCore(prog.staticProgram)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Fatal(err)
	}
	if !m.Typed || typeString(m.Types["P"]) != "P" {
		t.Fatalf("typed %v, types %v", m.Typed, m.Types)
	}
	if len(m.Vars) != 1 || m.Vars[0].Name != "k" || typeString(m.Vars[0].Type) != "int" || m.Vars[0].Init != 2 {
		t.Errorf("vars %+v", m.Vars)
	}
	main, pair := m.Rules[0], m.Rules[1]
	if typeString(main.Type) != "P" || typeString(pair.Type) != "P" {
		t.Errorf("rule types %s %s", typeString(main.Type), typeString(pair.Type))
	}
	slotTypes := func(s []core.Slot) string {
		var out []string
		for _, x := range s {
			out = append(out, x.Name+" "+typeString(x.Type))
		}
		return strings.Join(out, ", ")
	}
	// A capture of a call has the type of the rule's node.
	if got := slotTypes(main.Scope); got != `p pair, xs node[[]node[(pair,), {"q": 0}]]` {
		t.Errorf("main scope %s", got)
	}
	if got := slotTypes(pair.Scope); got != "n node[[]node[string]], s node[string]" {
		t.Errorf("pair scope %s", got)
	}
	var element []core.Slot
	core.Walk(main.Body, func(n core.Node) {
		if r, ok := n.(*core.Repeat); ok && r.Scope != nil {
			element = r.Scope
		}
	})
	if got := slotTypes(element); got != "q pair" {
		t.Errorf("element scope %s", got)
	}
	var terms []string
	core.WalkTerm(main.Finish.Action, func(x core.Term) {
		terms = append(terms, core.TermString(x)+" : "+typeString(core.TypeOf(x)))
	})
	if len(terms) == 0 || !strings.HasSuffix(terms[0], " : P") {
		t.Errorf("action terms %v", terms)
	}
	for _, s := range terms {
		if strings.HasSuffix(s, " : <nil>") {
			t.Errorf("untyped term %s", s)
		}
	}
	if !slices.Contains(terms, "var:k : int") && !slices.ContainsFunc(terms, func(s string) bool { return strings.HasSuffix(s, "k : int") }) {
		t.Errorf("variable term not typed int: %v", terms)
	}
}

// TestCoreBuildStreamAndMissing checks that #stream at the top of a start rule builds a Stream,
// elsewhere a repetition, and that #missing builds a Missing of its literal.
func TestCoreBuildStreamAndMissing(t *testing.T) {
	prog := coreProgram(t, `@start
rule main = x:item* #stream $$
rule item = "a" ";" #missing / inner
rule inner = "b"* #stream
`)
	m, err := buildCore(prog.staticProgram)
	if err != nil {
		t.Fatal(err)
	}
	core.Analyze(m)
	if err := core.Verify(m); err != nil {
		t.Fatal(err)
	}
	got := core.Print(m, false)
	for _, want := range []string{
		"    capture 0 x\n      stream\n        repeat 0 -1\n",
		"      seq\n        lit \"a\"\n        missing\n          lit \";\"\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Print does not contain %q:\n%s", want, got)
		}
	}
	if _, ok := m.Rules[2].Body.(*core.Repeat); !ok {
		t.Errorf("inner: %T, want the repetition of a rule that starts no parse", m.Rules[2].Body)
	}
	if m.Rules[0].Body.Attrs().Eff.Set&core.EffCommit == 0 || m.Rules[2].Body.Attrs().Eff.Set&core.EffCommit != 0 {
		t.Errorf("commit effects: main %v, inner %v", m.Rules[0].Body.Attrs().Eff, m.Rules[2].Body.Attrs().Eff)
	}
}

// TestCoreBuildCallLevels checks the minimum binding level of calls of an Operators Rule, and that
// a level the rule lacks, or a level of a rule that has none, is an error.
func TestCoreBuildCallLevels(t *testing.T) {
	prog := coreProgram(t, `rule main = level(e, product) "+" e
rule e = #operators {
    operand [0-9]
    level sum { infix left "+" }
    level product { infix left "*" }
}
`)
	m, err := buildCore(prog.staticProgram)
	if err != nil {
		t.Fatal(err)
	}
	var levels []int
	core.Walk(m.Rules[0].Body, func(n core.Node) {
		if c, ok := n.(*core.Call); ok {
			levels = append(levels, c.Level)
		}
	})
	if !slices.Equal(levels, []int{1, 0}) {
		t.Errorf("levels %v, want [1 0]", levels)
	}
	e := prog.RuleList[1]
	if _, err := callLevel(&grammar.Ref{Name: "e", Level: "power"}, e); err == nil {
		t.Error("unknown level: no error")
	}
	if _, err := callLevel(&grammar.Ref{Name: "main", Level: "sum"}, prog.RuleList[0]); err == nil {
		t.Error("level of a rule without levels: no error")
	}
}
