package engine

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
)

// The generated name of an inline action's rule never satisfies a reference: a reference resolves
// against the rules declared in the grammar as written.
func TestGeneratedNamesDoNotSatisfyReferences(t *testing.T) {
	t.Parallel()
	msg := compileError(t, `rule main = (x:"a" => $x) _main_1`)
	if !strings.Contains(msg, "undefined rule _main_1") {
		t.Fatal(msg)
	}
	// A declared rule that has the generated name keeps its name, and the inline action's rule takes
	// another one.
	p := mustCompile(t, `rule main = (x:"a" => $x) _main_1
rule _main_1 = "b"`)
	if got := result(p, "ab"); strings.Contains(got, "error") {
		t.Fatal(got)
	}
	names := map[string]bool{}
	for _, r := range p.RuleList {
		names[r.Name] = true
	}
	if !names["_main_1"] || !names["_main_1_"] {
		t.Fatalf("rules: %v", names)
	}
	if p.RuleList[len(p.RuleList)-1].Name != "_main_1" && p.ByName["_main_1_"].Def.Action == nil {
		t.Fatal("the inline action's rule is not the one that has the action")
	}
	if p.ByName["_main_1"].Def.Action != nil {
		t.Fatal("the declared rule was replaced")
	}
}

// The grammar of a program is the grammar as written in every mode: the inline actions stay in it
// and its rules are the declared ones, also when the program makes default values.
func TestSourceGrammarKeepsInlineActionsInEveryMode(t *testing.T) {
	t.Parallel()
	src := `type A = { X node[string] }
rule main = (x:"a" => A{X: $x}) _b
rule _b = "b"`
	for _, opts := range []compile.Options{{}, {DefaultValue: true}, {SourceInfo: true}} {
		p := compileBare(t, src, opts)
		g := p.SourceGrammar()
		inline := 0
		for _, rd := range g.Rules() {
			if strings.HasPrefix(rd.Name, "_main") {
				t.Errorf("%+v: the grammar as written has the generated rule %s", opts, rd.Name)
			}
			compile.WalkExpr(rd.Expr, func(e grammar.Expr) {
				if _, ok := e.(*grammar.InlineAction); ok {
					inline++
				}
			})
		}
		if inline != 1 || len(g.Rules()) != 2 {
			t.Errorf("%+v: %d inline actions and %d rules in the grammar as written", opts, inline, len(g.Rules()))
		}
		if len(p.Grammar.Rules()) != 3 {
			t.Errorf("%+v: the lowered grammar has %d rules", opts, len(p.Grammar.Rules()))
		}
		// The compiled form keeps it and loads to the same behavior.
		data, err := p.MarshalBinary("main")
		if err != nil {
			t.Fatal(err)
		}
		loaded, _, err := LoadProgram(data)
		if err != nil {
			t.Fatalf("%+v: %v", opts, err)
		}
		if got, want := gentest.ResultJSON(loaded.Parse("main", "ab")), gentest.ResultJSON(p.Parse("main", "ab")); got != want {
			t.Errorf("%+v: loaded %s, compiled %s", opts, got, want)
		}
	}
}
