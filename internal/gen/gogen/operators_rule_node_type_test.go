package gogen_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// Typed Go yields *Expr for every operator node of an Operators Rule declared with Expr, at every
// nesting level.
func TestGeneratedTypedOperatorsRuleNamedNodeType(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedOperatorsRuleNamedNodeType)
}

func testGeneratedTypedOperatorsRuleNamedNodeType(t *testing.T) {
	runGeneratedNamedNodeProgram(t, gentest.OperatorsNamedNodeCases()[0].Src, `func walk(v any) string {
	x, ok := v.(*p.Expr)
	if !ok {
		return fmt.Sprintf("%T", v)
	}
	s := "Expr"
	if kids, ok := x.Value.([]any); ok {
		s += "("
		for i, k := range kids {
			if i > 0 {
				s += " "
			}
			s += walk(k)
		}
		s += ")"
	}
	return s
}

func main() {
	for _, in := range []string{"1", "1+2", "-1+2", "2^3^4", "1 + (2 - 3) + 4", "1[2]!", "1+2*3"} {
		out, err := p.ParseAST(in)
		if err != nil {
			panic(err)
		}
		fmt.Println(in, walk(out))
	}
}
`, `1 Expr
1+2 Expr(Expr *p.TNode[string] Expr)
-1+2 Expr(Expr(*p.TNode[string] Expr) *p.TNode[string] Expr)
2^3^4 Expr(Expr *p.TNode[string] Expr(Expr *p.TNode[string] Expr))
1 + (2 - 3) + 4 Expr(Expr(Expr *p.TNode[string] Expr(Expr *p.TNode[string] Expr)) *p.TNode[string] Expr)
1[2]! Expr(Expr *p.TNode[interface {}])
1+2*3 Expr(Expr *p.TNode[string] Expr)
`)
}

// An operator whose value does not fit the named node type is a compile error, the tuple node of
// an operator without an action included, as the value of an ordinary rule is. Generated parsers
// reject these grammars when they are generated.
func TestOperatorsRuleNamedNodeTypeRuntimeError(t *testing.T) {
	t.Parallel()
	const defs = "type Expr = node[string]\nrule num Expr = [0-9]+ #atomic\n"
	for _, tc := range []struct{ src, static string }{
		{"rule expr Expr = #operators {\n operand num\n level { infix left \"+\" => 1 }\n}", "operatorsRule expr produces int, which cannot be converted to Expr"},
		{"rule expr Expr = #operators {\n operand num\n level { infix left \"+\" }\n}", "operators of expr without an action produce a tuple node"},
		{"rule expr Expr = #operators {\n operand num\n level { prefix \"-\" }\n}", "operators of expr without an action produce a tuple node"},
	} {
		if got := compileMessage(t, defs+tc.src); !strings.Contains(got, tc.static) {
			t.Errorf("%q: checked compilation: got %q, want an error containing %q", tc.src, got, tc.static)
		}
		g, err := parseTest(defs + tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gentest.Generate(t, g, gen.GenOptions{Package: "p", Start: "expr"}); err == nil {
			t.Errorf("%q: the generator accepted a grammar that cannot be type checked", tc.src)
		}
	}
}
