package engine

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

func typedFramedCutCases(t *testing.T) []genCase {
	t.Helper()
	var cases []genCase
	for _, c := range typedDirectCutCases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range g.Rules() {
			if r.Name != "main" || !directCuts(r.Expr) {
				continue
			}
			// Move the existing expression and action into a runtime-owned
			// operand frame, exercising the same cut/rollback contracts.
			r.Expr = &grammar.Pratt{Operands: []*grammar.PrattOperand{{Expr: r.Expr, Action: r.Action}}}
			r.Action = nil
			cases = append(cases, genCase{strings.Replace(c.name, "direct_cut", "framed_cut", 1), grammar.Format(g), c.inputs})
		}
	}
	cases = append(cases, genCase{"typed/framed_cut_LR_elements", `
type Doc struct { Items []Match, N int }
def main: Doc = e:expr $$ -> $e
def expr: Doc = (previous:expr ";" -- items:(x:@(?a-z) -- -","){1,3}
              / items:(x:@(?a-z) -- -","){1,3})
                -> new Doc{Items: map($items, (e) => $e.x), N: len($items)}
`, []string{"a,", "a,b,;c,d,", "a,;", "a", "a,b", "a,;c", "a,;c,d,?", ""}},
		genCase{"typed/framed_cut_Pratt_longest", `
type Atom struct { Text string }
type Prefix struct { X Expr, Tag int, Text Match }
type Expr = Atom | Prefix
def main: Expr = [n = 0] e:expr $$ -> $e
def expr: Expr = pratt {
    skip (" " -- "!")*
    operand x:@"a" -- -> new Prefix{X: new Atom{Text: "a"}, Tag: n, Text: $x}
    operand x:@"-x" -- -> new Prefix{X: new Atom{Text: "-x"}, Tag: n, Text: $x}
    level {
        prefix o:@"-" -- [n = 1] "z" -> new Prefix{X: $rhs, Tag: n, Text: $o}
        prefix o:@"--" -- [n = 2] -> new Prefix{X: $rhs, Tag: n, Text: $o}
        prefix o:@"--" -- [n = 3] -> new Prefix{X: $rhs, Tag: n, Text: $o}
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
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := GenOptions{Package: "framedcuts", Start: "main", Types: true, Recognize: true}
		code, err := Generate(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		marker := "(typed Pratt body inlined)"
		if c.name == "typed/framed_cut_LR_elements" {
			marker = "(typed LR body inlined)"
		}
		if !strings.Contains(string(code), "tparse(trules") || !strings.Contains(string(code), marker) {
			t.Fatalf("%s: cut-bearing typed body must be exercised", c.name)
		}
		opts.disableTypedFramedCuts = true
		ref, err := Generate(g, opts)
		if err != nil || strings.Contains(string(ref), marker) || strings.Contains(string(code), "disableTypedFramedCuts") {
			t.Fatalf("%s: invalid generation-only cut control: %v", c.name, err)
		}
		opts.Types = false
		nodeRef, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedFramedCuts = false
		node, err := Generate(g, opts)
		if err != nil || !bytes.Equal(nodeRef, node) {
			t.Fatalf("%s: framed-cut control changes Node output: %v", c.name, err)
		}
	}
	cases = append(cases, prefixPartCutCorpus())
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{disableTypedFramedCuts: true})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true})
}
