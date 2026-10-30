package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

// operatorsNodeTypeNames returns the type names of the nodes of n, pre-order, with their children
// in parentheses.
func operatorsNodeTypeNames(n *value.Node) string {
	if n == nil {
		return "nil"
	}
	var b strings.Builder
	b.WriteString(n.Type())
	if n.Len() > 0 && n.Type() != "string" {
		b.WriteString("(")
		for i := 0; i < n.Len(); i++ {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(operatorsNodeTypeNames(n.Elem(i)))
		}
		b.WriteString(")")
	}
	return b.String()
}

// TestOperatorsRuleNamedNodeTypeShape checks the type of every node an Operators Rule builds: the
// operator nodes are converted at every nesting level; the nodes that an operator part carries (the
// value of $op) are not operator nodes and keep their types.
func TestOperatorsRuleNamedNodeTypeShape(t *testing.T) {
	t.Parallel()
	cases := gentest.OperatorsNamedNodeCases()
	for _, sourceInfo := range []bool{false, true} {
		{
			prog := compileBare(t, cases[0].Src, compile.Options{SourceInfo: sourceInfo})
			for _, tc := range []struct{ input, want string }{
				{"1", "Expr"},
				{"1+2", "Expr(Expr string Expr)"},
				{"1+2*3", "Expr(Expr string Expr)"},
				{"-1+2", "Expr(Expr(string Expr) string Expr)"},
				{"2!", "Expr"},
				{"2^3^4", "Expr(Expr string Expr(Expr string Expr))"},
				{"1[2][3]!", "Expr(Expr(Expr tuple(string Expr list string)) tuple(string Expr list string))"},
				{"1 + (2 - 3) + 4", "Expr(Expr(Expr string Expr(Expr string Expr)) string Expr)"},
			} {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					n, err := prog.ParseWith("main", tc.input, ParseOptions{Backend: backend})
					if err != nil {
						t.Fatalf("%s %q: %v", backend, tc.input, err)
					}
					if got := operatorsNodeTypeNames(n); got != tc.want {
						t.Errorf("sourceInfo=%t %s %q: got %s, want %s", sourceInfo, backend, tc.input, got, tc.want)
					}
				}
			}
		}
	}
}

// TestOperatorsRuleNamedNodeTypeAcrossBackends checks that the closure evaluator and the bytecode
// backends build the same trees for an Operators Rule with and without a named node type.
func TestOperatorsRuleNamedNodeTypeAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, c := range gentest.OperatorsNamedNodeCases() {
		for _, sourceInfo := range []bool{false, true} {
			{
				prog := compileBare(t, c.Src, compile.Options{SourceInfo: sourceInfo})
				for _, input := range c.Inputs {
					want := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: Closure}))
					for _, backend := range []Backend{Bytecode, BytecodeIterative} {
						if got := gentest.ResultJSON(prog.ParseWith("main", input, ParseOptions{Backend: backend})); got != want {
							t.Errorf("%s sourceInfo=%t %s %q:\n got %s\nwant %s", c.Name, sourceInfo, backend, input, got, want)
						}
					}
				}
			}
		}
	}
}

// A Document reparses an Operators Rule with a named node type to the trees of a fresh parse,
// whose operator nodes are all converted, across edits.
func TestOperatorsRuleNamedNodeTypeDocument(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.OperatorsNamedNodeCases()[0].Src)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		opts := ParseOptions{Backend: backend}
		doc, err := prog.NewDocumentWith("main", "1+2*3+(4-5)!", opts)
		if err != nil {
			t.Fatal(err)
		}
		for i, e := range []struct {
			from, to int
			text     string
		}{{0, 0, ""}, {2, 3, "-9"}, {5, 6, "^7"}, {0, 0, "-"}, {3, 4, ""}, {1, 2, "*"}} {
			if err := doc.Edit(e.from, e.to, e.text); err != nil {
				t.Fatal(err)
			}
			n, err := doc.Parse()
			fresh, ferr := prog.ParseWith("main", doc.Text(), opts)
			if got, want := gentest.ResultJSON(n, err), gentest.ResultJSON(fresh, ferr); got != want {
				t.Fatalf("%s edit %d %q: got %s; fresh %s", backend, i, doc.Text(), got, want)
			}
			if err == nil && strings.Contains(operatorsNodeTypeNames(n), "tuple(") && !strings.Contains(doc.Text(), "[") {
				t.Errorf("%s edit %d %q: an operator node was not converted: %s", backend, i, doc.Text(), operatorsNodeTypeNames(n))
			}
		}
	}
}

// An Operators Rule without a named node type keeps the tuple nodes of its operators.
func TestOperatorsRuleWithoutNamedNodeTypeKeepsTuples(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, gentest.OperatorsNamedNodeCases()[1].Src)
	n, err := prog.Parse("main", "-1+2!")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := operatorsNodeTypeNames(n), "tuple(tuple(string string) string tuple(string string))"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

