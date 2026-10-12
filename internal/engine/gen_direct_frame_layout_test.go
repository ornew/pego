package engine

import (
	"bytes"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

var typedFrameLayoutCases = []genCase{
	{"typed/LR_dead_capture_order", `type Doc struct { Text *Match, Missing *Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = _|_ unused:"z" #atomic / p:expr ";" x:"é" #atomic / x:"a" #atomic / _|_ ignored:"b" #atomic => Doc{Text: $x, Missing: $ignored}
`, []string{"a", "a;é;é", "a;", "b", "z", "", "a?"}},
	{"typed/OperatorsRule_dead_capture_order", `type Doc struct { Text *Match, Missing *Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand _|_ before:"b" #atomic / x:"é" #atomic / _|_ after:"z" #atomic => Doc{Text: $x, Missing: $after}
}
`, []string{"é", "b", "z", "", "é?"}},
	{"typed/OperatorsRule_infallible_alternative", `type Doc struct { Missing *Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand _ / ignored:"b" #atomic => Doc{Missing: $ignored}
}
`, []string{"", "b", "é"}},
	{"typed/OperatorsRule_dead_element_predicate", `type Atom terminal
type Doc struct { Items []*Match, N int }
type Expr = Atom | Doc
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    operand atom
    level {
        postfix "[" items:((_|_ ignored:"z" #atomic / x:[a-z] #atomic) {len($x) == 1 && $ignored == nil} -",") #repeat(3) "]" => Doc{Items: map($items, (e) => $e.x), N: len($items)}
    }
}
rule atom Atom = "a"
`, []string{"a", "a[b,c,d,]", "a[b,c,]", "a[b,é,d,]", "a[b,c,d,", "a[b,c,d,]?", ""}},
	{"typed/OperatorsRule_dead_projection", `type Doc struct { Items []*Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand _|_ ignored:"z" #atomic / items:((_|_ unused:"z" #atomic / x:[a-z] #atomic) {$unused == nil} -",") #repeat(3) => Doc{Items: map($items, (e) => $e.x)}
}
`, []string{"a,b,c,", "a,b,", "a,b,é,", "z", "", "a,b,c,?"}},
	{"typed/OperatorsRule_dead_lookahead_capture", `type Doc struct { Text *Match, Missing *Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand &(_|_ before:"z" #atomic / x:"é" #atomic) x:"é" #atomic / _|_ after:"z" #atomic => Doc{Text: $x, Missing: $after}
}
`, []string{"é", "z", "", "é?"}},
}

func TestGeneratedTypedFrameLayouts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	cases := append([]genCase(nil), typedFrameLayoutCases...)
	for _, c := range typedLRBodyCases {
		if c.name == "typed/LR_unreachable_capture" {
			cases = append(cases, c)
		}
	}
	for _, c := range typedOperatorsRuleBodyCases {
		if c.name == "typed/OperatorsRule_unreachable_capture" {
			cases = append(cases, c)
		}
	}
	for _, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := GenOptions{Package: "layouts", Start: "main", Types: true, Recognize: true}
		candidate, err := Generate(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		marker := "(typed OperatorsRule body inlined)"
		if strings.Contains(c.name, "/LR_") {
			marker = "(typed LR body inlined)"
		}
		if !strings.Contains(string(candidate), marker) {
			t.Fatalf("%s: candidate route not exercised", c.name)
		}
		opts.disableTypedFrameLayouts = true
		reference, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		// The element case has an independent eligible operand; its postfix
		// must fall back. Other cases have one affected LR/OperatorsRule matcher.
		wantCount := 0
		if c.name == "typed/OperatorsRule_dead_element_predicate" {
			wantCount = 1
		}
		if strings.Count(string(reference), marker) != wantCount || strings.Contains(string(candidate), "disableTypedFrameLayouts") {
			t.Fatalf("%s: invalid generation-only control", c.name)
		}
		opts.Types = false
		nodeReference, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedFrameLayouts = false
		node, err := Generate(g, opts)
		if err != nil || !bytes.Equal(nodeReference, node) {
			t.Fatalf("%s: typed layout control changes Node output: %v", c.name, err)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{disableTypedFrameLayouts: true})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true})
}

// Compare with the actual general emitter's metadata, including unused
// captures under atomic/discard/lookahead and projected element boundaries.
func TestTypedFrameLayoutMetadata(t *testing.T) {
	cases := append([]genCase(nil), typedFrameLayoutCases...)
	cases = append(cases, typedLRBodyCases...)
	cases = append(cases, typedOperatorsRuleBodyCases...)
	cases = append(cases, typedDirectCutCases...)
	cases = append(cases, typedRecoveryCases...)
	cases = append(cases, typedLocalLayoutCases...)
	for _, c := range cases {
		p := compile(t, c.src)
		for _, r := range append(append([]*rule(nil), p.rules...), p.twins...) {
			var exprs []grammar.Expr
			walkExpr(r.def.Expr, func(e grammar.Expr) {
				if directFrameExprOK(e, true, true) {
					exprs = append(exprs, e)
				}
			})
			for _, e := range exprs {
				for _, build := range []bool{false, true} {
					g := &generator{prog: p, table: "trules", cur: r}
					g.proj = g.projections(r)
					actual := newScope()
					g.expr(e, actual, build)
					layout := g.typedFrameLayout(e, build)
					if !slices.Equal(actual.names, layout.names) {
						t.Fatalf("%s/%s build=%v %T: general=%v layout=%v", c.name, r.name, build, e, actual.names, layout.names)
					}
				}
			}
		}
	}
}
