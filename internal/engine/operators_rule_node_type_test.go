package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/value"
)

// operatorsNamedNodeCases declare an Operators Rule with a named node type: every prefix, infix and
// postfix node the rule builds, at every nesting level, is converted to that type, whether it is
// the tuple node of an operator without an action or the node that an action returns. The operands
// come from other rules that convert their own values.
func operatorsNamedNodeCases() []gentest.Case {
	return []gentest.Case{
		{Name: "operators named tuple", Src: `type Expr = node[_]
type Other = node[string]
rule main Expr = e:expr ws $$ => $e
rule expr Expr = #operators {
    skip ws
    operand num
    operand "(" e:expr ws ")" => $e
    level { infix left "+" / "-" }
    level { infix left "*" => Other{$op} }
    level { prefix "-" }
    level {
        postfix "!" => $lhs
        postfix "[" i:expr ws "]"
    }
    level { infix right "^" }
}
rule num Expr = [0-9]+ #atomic
rule ws = [ ]*`, Inputs: []string{"", "1", "1+2", "1+2*3", "-1+2", "1+-2", "2!", "(1+2)[3]*4", "1+2!+3", "1 + (2 - 3) * 4", "2^3^4", "-2^3", "1+", "-", "1[2][3]!", "((1))"}},
		{Name: "operators without a named type", Src: `type Expr ~ node[_]
rule main = e:expr ws $$ => $e
rule expr Expr = #operators {
    skip ws
    operand num
    operand "(" e:expr ws ")" => $e
    level { infix left "+" / "-" }
    level { prefix "-" }
    level { postfix "!" }
}
rule num = [0-9]+ #atomic
rule ws = [ ]*`, Inputs: []string{"", "1", "1+2", "-1+2!", "(1+2)!"}},
		{Name: "operators named through an alias", Src: `type Expr = node[_]
type Alias ~ Expr
rule main Alias = e:expr ws $$ => $e
rule expr Alias = #operators {
    skip ws
    operand num
    operand "(" e:expr ws ")" => $e
    level { infix left "+" / "-" }
    level { prefix "-" }
    level { postfix "!" }
}
rule num Alias = [0-9]+ #atomic
rule ws = [ ]*`, Inputs: []string{"", "1", "1+2", "-1+2!", "(1+2)!", "1+"}},
	}
}

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
	cases := operatorsNamedNodeCases()
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
	for _, c := range operatorsNamedNodeCases() {
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
	prog := mustCompile(t, operatorsNamedNodeCases()[0].Src)
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

// Generated Go parsers, plain and typed, and generated TypeScript parsers build the same trees as
// the engine for Operators Rules with and without a named node type.
func TestOperatorsRuleNamedNodeTypeGenerated(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testOperatorsRuleNamedNodeTypeGenerated)
}

func testOperatorsRuleNamedNodeTypeGenerated(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated consumers")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cases := operatorsNamedNodeCases()
	t.Run("go", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{}) })
	t.Run("go source info", func(t *testing.T) { gentest.GeneratedParsersCorpus(t, goBin, cases, gen.GenOptions{SourceInfo: true}) })
	t.Run("typed go", func(t *testing.T) {
		gentest.GeneratedTypesCorpus(t, goBin, cases, gen.GenOptions{ConvertTypes: true}, len(cases))
	})
	t.Run("typescript", func(t *testing.T) {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node not found")
		}
		testGeneratedTSParsersCorpus(t, node, cases, gen.GenOptions{})
		testGeneratedTSParsersCorpus(t, node, cases, gen.GenOptions{SourceInfo: true})
	})
}

// Typed Go yields *Expr for every operator node of an Operators Rule declared with Expr, at every
// nesting level.
func TestGeneratedTypedOperatorsRuleNamedNodeType(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testGeneratedTypedOperatorsRuleNamedNodeType)
}

func testGeneratedTypedOperatorsRuleNamedNodeType(t *testing.T) {
	runGeneratedNamedNodeProgram(t, operatorsNamedNodeCases()[0].Src, `func walk(v any) string {
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

// An Operators Rule without a named node type keeps the tuple nodes of its operators.
func TestOperatorsRuleWithoutNamedNodeTypeKeepsTuples(t *testing.T) {
	t.Parallel()
	prog := compileBare(t, operatorsNamedNodeCases()[1].Src)
	n, err := prog.Parse("main", "-1+2!")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := operatorsNodeTypeNames(n), "tuple(tuple(string string) string tuple(string string))"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

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
		if _, err := Compile(g, compile.Options{}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
}
