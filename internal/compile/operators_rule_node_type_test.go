package compile

import (
	"strings"
	"testing"
)

// An operator whose node cannot be converted to the rule's named node type is a compile-time
// error, as for the value of an ordinary rule.
func TestOperatorsRuleNamedNodeTypeErrors(t *testing.T) {
	t.Parallel()
	const defs = "type Expr = node[string]\ntype S = { A int }\nrule num Expr = [0-9]+ #atomic\n"
	for _, tc := range []struct{ src, want string }{
		{"rule expr Expr = #operators {\n operand num\n level { infix left \"+\" }\n}", "operators of expr without an action produce a tuple node, which cannot be converted to Expr"},
		{"rule expr Expr = #operators {\n operand num\n level { infix left \"+\" => S{A: 1} }\n}", "operatorsRule expr produces S, which cannot be converted to Expr"},
		{"rule expr Expr = #operators {\n operand num\n level { prefix \"-\" => 1 }\n}", "which cannot be converted to Expr"},
		{"type Num = node[string]\nrule expr Expr = #operators {\n operand n2\n level { infix left \"+\" => Expr{$op} }\n}\nrule n2 Num = [0-9]+ #atomic", "operand of expr produces Num, which is not assignable to Expr; an operand is not converted: declare the operand rule as Expr, or declare expr as a join that includes Num"},
	} {
		g, err := parseTest(defs + tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
}
