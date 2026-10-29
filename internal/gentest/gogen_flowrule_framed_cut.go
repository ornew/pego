package gentest

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compiletest"
)

func TypedFramedCutCases(t *testing.T) []Case {
	t.Helper()
	var cases []Case
	for _, c := range TypedDirectCutCases {
		g, err := compiletest.Parse(c.Src)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range g.Rules() {
			if r.Name != "main" || !DirectCuts(r.Expr) {
				continue
			}
			// Move the existing expression and action into a runtime-owned
			// operand frame, exercising the same cut/rollback contracts.
			r.Expr = &grammar.OperatorsRule{Operands: []*grammar.OperatorsRuleOperand{{Expr: r.Expr, Action: r.Action}}}
			r.Action = nil
			cases = append(cases, Case{strings.Replace(c.Name, "direct_cut", "framed_cut", 1), grammar.Format(g), c.Inputs})
		}
	}
	cases = append(cases, Case{"typed/framed_cut_LR_elements", `type Doc = { Items []node[string]; N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = (
    previous:expr ";" #cut items:(x:[a-z] #atomic #cut -",") #repeat(1, 3) / items:(x:[a-z] #atomic #cut -",") #repeat(1, 3)
) => Doc{Items: map($items, (e) => e.$x), N: len($items)}
`, []string{"a,", "a,b,;c,d,", "a,;", "a", "a,b", "a,;c", "a,;c,d,?", ""}},
		Case{"typed/framed_cut_OperatorsRule_longest", `type Atom = { Text string }
type Prefix = { X Expr; Tag int; Text node[string] }
type Expr ~ Atom | Prefix
var n int
rule main Expr = {n := 0} e:expr $$ => $e
rule expr Expr = #operators {
    skip (" " #cut "!")*
    operand x:"a" #atomic #cut => Prefix{X: Atom{Text: "a"}, Tag: n, Text: $x}
    operand x:"-x" #atomic #cut => Prefix{X: Atom{Text: "-x"}, Tag: n, Text: $x}
    level {
        prefix o:"-" #atomic #cut {n := 1} "z" => Prefix{X: $rhs, Tag: n, Text: $o}
        prefix o:"--" #atomic #cut {n := 2} => Prefix{X: $rhs, Tag: n, Text: $o}
        prefix o:"--" #atomic #cut {n := 3} => Prefix{X: $rhs, Tag: n, Text: $o}
    }
}
`, []string{"a", "-x", "-za", "--a", "---a", "-z", "--", " !a", " a", "-x?", ""}})
	return cases
}
