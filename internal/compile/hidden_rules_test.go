package compile

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// A grammar that default-value mode accepts because it drops the actions is accepted with inline
// actions as well: the grammar as written is not validated for a program that does not run its
// actions.
func TestDefaultValueAcceptsWhatItDropsWithInlineActions(t *testing.T) {
	t.Parallel()
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
			WalkExpr(rd.Expr, func(e grammar.Expr) {
				if a, ok := e.(*grammar.InlineAction); ok {
					a.Action = bad
				}
			})
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil {
			t.Errorf("%s: compiled without the option", src)
		}
		if _, err := compileStatic(g, Options{DefaultValue: true}, nil); err != nil {
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
	t.Parallel()
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
				WalkExpr(rd.Expr, func(e grammar.Expr) {
					if r, ok := e.(*grammar.Repeat); ok {
						r.Min = -1
						repeats++
					}
				})
			}
			if repeats != 1 {
				t.Fatalf("%d repetitions to corrupt", repeats)
			}
			_, withActions := compileStatic(g, Options{}, nil)
			_, withDefault := compileStatic(g, Options{DefaultValue: true}, nil)
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
	t.Parallel()
	names := InlineNames{"_main_1": {rule: "main", n: 1}, "_main_10": {rule: "main", n: 10}}
	for _, c := range []struct{ in, want string }{
		{"rule _main_1 produces int", "the inline action in rule main produces int"},
		{"result of rule _main_10: _main_1", "result of the inline action in rule main: the inline action in rule main"},
		{"_main_1_ is not _main_1", "_main_1_ is not the inline action in rule main"},
	} {
		if got := names.Text(c.in); got != c.want {
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
	t.Parallel()
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
	t.Parallel()
	p := mustCompile(t, "rule main = \"a\" _m\nrule _m = \"b\" _m?")
	rules := p.Grammar.Rules()
	for _, c := range []struct {
		name string
		main Ty
		kept bool
	}{
		{"element", TupleTy{Elems: []Ty{TyStringNode, OptTy{Elem: RuleTy{Name: "_m"}}}}, true},
		{"list", ListTy{Elem: RuleTy{Name: "_m"}}, true},
		{"join", JoinTy{Alts: []Ty{TyStringNode, RuleTy{Name: "_m"}}}, true},
		{"capture", NodeTy{Value: TyStringNode, Caps: []CapIndex{{Name: "x", Paths: [][]int{{0}}, T: RuleTy{Name: "_m"}}}}, true},
		{"capture set", NodeTy{Value: TyStringNode, Caps: []CapIndex{{Name: "x", Paths: [][]int{{0}}, T: TyStringNode, maybe: true, set: RuleTy{Name: "_m"}}}}, true},
		{"no reference", TupleTy{Elems: []Ty{TyStringNode}}, false},
		{"a name that holds the rule's", RuleTy{Name: "x_m"}, false},
	} {
		out := pruneSpliceTypes(p, rules, map[string]Ty{"main": c.main, "_m": TyStringNode})
		if _, got := out["_m"]; got != c.kept {
			t.Errorf("%s: kept %v, want %v", c.name, got, c.kept)
		}
	}
}
