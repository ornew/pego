package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// The generated name of an inline action's rule never satisfies a reference: a reference resolves
// against the rules declared in the grammar as written.
func TestGeneratedNamesDoNotSatisfyReferences(t *testing.T) {
	msg := compileError(t, `rule main = (x:"a" => $x) _main_1`)
	if !strings.Contains(msg, "undefined rule _main_1") {
		t.Fatal(msg)
	}
	// A declared rule that has the generated name keeps its name, and the inline action's rule takes
	// another one.
	p := compile(t, `rule main = (x:"a" => $x) _main_1
rule _main_1 = "b"`)
	if got := result(p, "ab"); strings.Contains(got, "error") {
		t.Fatal(got)
	}
	names := map[string]bool{}
	for _, r := range p.rules {
		names[r.name] = true
	}
	if !names["_main_1"] || !names["_main_1_"] {
		t.Fatalf("rules: %v", names)
	}
	if p.rules[len(p.rules)-1].name != "_main_1" && p.byName["_main_1_"].def.Action == nil {
		t.Fatal("the inline action's rule is not the one that has the action")
	}
	if p.byName["_main_1"].def.Action != nil {
		t.Fatal("the declared rule was replaced")
	}
}

// A hidden rule is not an entry point: not the start rule, not a rule parsed by name.
func TestHiddenRulesAreNotEntryPoints(t *testing.T) {
	p := compile(t, `rule main = _a "c"
rule _a = "a" "b"`)
	for _, name := range p.Rules() {
		if strings.HasPrefix(name, "_") {
			t.Errorf("Rules lists the hidden rule %s", name)
		}
	}
	if _, err := p.Parse("_a", "ab"); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("parse by a hidden name: %v", err)
	}
	if _, err := p.MarshalBinary("_a"); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("marshal with a hidden start: %v", err)
	}
	if _, err := p.OutputSchemaPrototype("_a", false); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("schema of a hidden rule: %v", err)
	}
	data, err := p.MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProgram(data); err != nil {
		t.Fatal(err)
	}
	// The same holds for the rule of an inline action, whose name nobody declared.
	q := compile(t, `rule main = (x:"a" => $x) "b"`)
	if _, err := q.Parse("_main_1", "a"); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("parse by a generated name: %v", err)
	}
	for _, g := range []struct{ src, start string }{
		{`rule main = _a
rule _a = "a"`, "_a"},
		{`rule main = (x:"a" => $x)`, "_main_1"},
	} {
		parsed, err := parseTest(g.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Generate(parsed, GenOptions{Module: "m", Start: g.start}); err == nil || !strings.Contains(err.Error(), "hidden") {
			t.Errorf("Go generation with the start rule %s: %v", g.start, err)
		}
		if _, err := GenerateTS(parsed, GenOptions{Start: g.start}); err == nil || !strings.Contains(err.Error(), "hidden") {
			t.Errorf("TypeScript generation with the start rule %s: %v", g.start, err)
		}
	}
}

// The grammar of a program is the grammar as written in every mode: the inline actions stay in it
// and its rules are the declared ones, also when the program makes default values.
func TestSourceGrammarKeepsInlineActionsInEveryMode(t *testing.T) {
	src := `type A = { X node[string] }
rule main = (x:"a" => A{X: $x}) _b
rule _b = "b"`
	for _, opts := range []Options{{}, {DefaultValue: true}, {SourceInfo: true}} {
		p := compileBare(t, src, opts)
		g := p.SourceGrammar()
		inline := 0
		for _, rd := range g.Rules() {
			if strings.HasPrefix(rd.Name, "_main") {
				t.Errorf("%+v: the grammar as written has the generated rule %s", opts, rd.Name)
			}
			walkExpr(rd.Expr, func(e grammar.Expr) {
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
		if got, want := resultJSON(loaded.Parse("main", "ab")), resultJSON(p.Parse("main", "ab")); got != want {
			t.Errorf("%+v: loaded %s, compiled %s", opts, got, want)
		}
	}
}

// A grammar that default-value mode accepts because it drops the actions is accepted with inline
// actions as well: the grammar as written is not validated for a program that does not run its
// actions.
func TestDefaultValueAcceptsWhatItDropsWithInlineActions(t *testing.T) {
	for _, src := range []string{
		`rule main = "a" => nil`,
		`rule main = (x:"a" => nil) "b"`,
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		// An action that validation rejects, since an AST may hold what the syntax does not allow.
		bad := &grammar.Lambda{Params: []string{"_arg"}, Body: &grammar.NilLit{}}
		for _, rd := range g.Rules() {
			if rd.Action != nil {
				rd.Action = bad
			}
			walkExpr(rd.Expr, func(e grammar.Expr) {
				if a, ok := e.(*grammar.InlineAction); ok {
					a.Action = bad
				}
			})
		}
		if _, err := Compile(g, Options{}); err == nil {
			t.Errorf("%s: compiled without the option", src)
		}
		if _, err := Compile(g, Options{DefaultValue: true}); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

// A defect of the grammar that default-value mode still validates is reported where the grammar as
// written has it: with the numbering of its declarations, not of the lowered grammar that has the
// rules of the inline actions after the rule that holds them, and with the path inside an inline
// action, not inside a rule it never declared. The report is the one of the same grammar compiled
// with the actions.
func TestDefaultValueValidationReportsTheGrammarAsWritten(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      string // a part of the path
	}{
		{"after an inline action", "rule a = (x:\"a\" => nil)\nrule main = \"b\"+", "decls[1]"},
		{"after two inline actions", "rule a = (x:\"a\" => nil) (y:\"c\" => nil)\nrule b = (z:\"a\" => nil)\nrule main = \"b\"+", "decls[2]"},
		{"in an inline action", "rule a = \"c\"\nrule main = \"b\" (x:\"a\"+ => nil) \"d\"", "decls[1]"},
		{"in a nested inline action", "rule main = (x:(y:\"a\"+ => nil) \"b\" => nil)", "decls[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := parseTest(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			rules := g.Rules()
			repeats := 0
			for _, rd := range rules {
				walkExpr(rd.Expr, func(e grammar.Expr) {
					if r, ok := e.(*grammar.Repeat); ok {
						r.Min = -1
						repeats++
					}
				})
			}
			if repeats != 1 {
				t.Fatalf("%d repetitions to corrupt", repeats)
			}
			_, withActions := Compile(g, Options{})
			_, withDefault := Compile(g, Options{DefaultValue: true})
			if withActions == nil || withDefault == nil {
				t.Fatalf("no error: %v / %v", withActions, withDefault)
			}
			if withDefault.Error() != withActions.Error() {
				t.Errorf("default value: %v\nwith actions:  %v", withDefault, withActions)
			}
			if !strings.Contains(withDefault.Error(), tc.want) || strings.Contains(withDefault.Error(), "_main") || strings.Contains(withDefault.Error(), "_a_") {
				t.Errorf("want the path to hold %s and no generated name: %v", tc.want, withDefault)
			}
		})
	}
}

// The messages of the compiler describe an inline action by the rule that holds it.
func TestInlineActionNamesInMessages(t *testing.T) {
	names := inlineNames{"_main_1": {rule: "main", n: 1}, "_main_10": {rule: "main", n: 10}}
	for _, c := range []struct{ in, want string }{
		{"rule _main_1 produces int", "the inline action in rule main produces int"},
		{"result of rule _main_10: _main_1", "result of the inline action in rule main: the inline action in rule main"},
		{"_main_1_ is not _main_1", "_main_1_ is not the inline action in rule main"},
	} {
		if got := names.text(c.in); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
	if got, ok := names.describe("main"); ok {
		t.Errorf("describes a declared rule: %q", got)
	}
}

// Inside a type, an inline action stands for the type of its result, not for the rule that holds
// it; a type in a message never shows a name the author did not write. Elsewhere the message names
// the rule that holds the inline action.
func TestInlineActionTypesInMessages(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"type T = { A node[string] }\nrule main T = (x:\"a\" \"c\" => $x) \"b\"", "rule main produces node[(node[string], node[string])], which is not assignable to T"},
		{"type T = { A string }\nrule main = i:(x:\"a\" => 1)* \"b\" => T{A: $i}", "cannot use node[[]int] as string in field A of T"},
		{"type T = { A string }\nrule main = i:(x:\"a\" => 1)? \"b\" => T{A: $i}", "cannot use *int as string in field A of T"},
		{"rule main string = (x:\"a\" => 1) (x:\"a\" => 1)", "rule main produces node[(int, int)], which is not assignable to string"},
		{"rule main string = (x:\"a\" => (1, \"s\")) (x:\"a\" => nil)", "rule main produces node[((int, string), nil)], which is not assignable to string"},
	} {
		msg := compileError(t, c.src)
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s:\n%s\nwant %q", c.src, msg, c.want)
		}
		if strings.Contains(msg, "(the inline action") || strings.Contains(msg, "[]the inline action") {
			t.Errorf("%s: an inline action inside a type: %s", c.src, msg)
		}
	}
}

// The rules a type refers to are found in the structure of the type, including the types of the
// captures of a node, which have no place in its text.
func TestPruneSpliceTypesFollowsTheStructureOfTypes(t *testing.T) {
	p := compile(t, "rule main = \"a\" _m\nrule _m = \"b\" _m?")
	rules := p.Grammar.Rules()
	for _, c := range []struct {
		name string
		main ty
		kept bool
	}{
		{"element", tupleTy{[]ty{tyStringNode, optTy{ruleTy{"_m"}}}}, true},
		{"list", listTy{ruleTy{"_m"}}, true},
		{"join", joinTy{[]ty{tyStringNode, ruleTy{"_m"}}}, true},
		{"capture", nodeTy{value: tyStringNode, caps: []capIndex{{name: "x", paths: [][]int{{0}}, t: ruleTy{"_m"}}}}, true},
		{"capture set", nodeTy{value: tyStringNode, caps: []capIndex{{name: "x", paths: [][]int{{0}}, t: tyStringNode, maybe: true, set: ruleTy{"_m"}}}}, true},
		{"no reference", tupleTy{[]ty{tyStringNode}}, false},
		{"a name that holds the rule's", ruleTy{"x_m"}, false},
	} {
		out := pruneSpliceTypes(p, rules, map[string]ty{"main": c.main, "_m": tyStringNode})
		if _, got := out["_m"]; got != c.kept {
			t.Errorf("%s: kept %v, want %v", c.name, got, c.kept)
		}
	}
}
