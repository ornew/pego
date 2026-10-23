package engine

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func typedFramedCutCases(t *testing.T) []genCase {
	t.Helper()
	var cases []genCase
	for _, c := range typedDirectCutCases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range g.Rules() {
			if r.Name != "main" || !directCuts(r.Expr) {
				continue
			}
			// Move the existing expression and action into a runtime-owned
			// operand frame, exercising the same cut/rollback contracts.
			r.Expr = &grammar.OperatorsRule{Operands: []*grammar.OperatorsRuleOperand{{Expr: r.Expr, Action: r.Action}}}
			r.Action = nil
			cases = append(cases, genCase{strings.Replace(c.name, "direct_cut", "framed_cut", 1), grammar.Format(g), c.inputs})
		}
	}
	cases = append(cases, genCase{"typed/framed_cut_LR_elements", `type Doc = { Items []node[string]; N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = (
    previous:expr ";" #cut items:(x:[a-z] #atomic #cut -",") #repeat(1, 3) / items:(x:[a-z] #atomic #cut -",") #repeat(1, 3)
) => Doc{Items: map($items, (e) => e.$x), N: len($items)}
`, []string{"a,", "a,b,;c,d,", "a,;", "a", "a,b", "a,;c", "a,;c,d,?", ""}},
		genCase{"typed/framed_cut_OperatorsRule_longest", `type Atom = { Text string }
type Prefix = { X Expr; Tag int; Text node[string] }
type Expr ~ Atom | Prefix
var n int
rule main Expr = {n := 0} e:expr $$ => $e
rule expr Expr = operators {
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

func TestGeneratedTypedFramedCuts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	cases := typedFramedCutCases(t)
	for _, c := range cases {
		g, err := parseTest(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := GenOptions{Package: "framedcuts", Start: "main", Types: true, Recognize: true}
		code, err := generateCode(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		marker := "(typed OperatorsRule body inlined)"
		if c.name == "typed/framed_cut_LR_elements" {
			marker = "(typed LR body inlined)"
		}
		if !strings.Contains(string(code), "TParse(trules") || !strings.Contains(string(code), marker) {
			t.Fatalf("%s: cut-bearing typed body must be exercised", c.name)
		}
		opts.Types = false
		nodeRef, err := generateCode(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedFramedCuts = false
		node, err := generateCode(g, opts)
		if err != nil || !bytes.Equal(nodeRef, node) {
			t.Fatalf("%s: framed-cut control changes Node output: %v", c.name, err)
		}
	}
	cases = append(cases, prefixPartCutCorpus())
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{disableTypedFramedCuts: true})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true})
}
