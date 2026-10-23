package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// The grammars of testdata/routes exercise the routes of typed rules that the Flow IR prints:
// left recursion with inline and recovering bodies, lookaheads that read leader captures, calls
// and recovery inside Operators Rules, and scopes of captures. Each is compared with dgen, built
// and run against the engine, and checked to be printed from Flow IR in every flowModes mode.

// routeInputs are the inputs of each route grammar, by file name without the extension.
var routeInputs = map[string][]string{
	"lr_recover":     {"1+2+3", "1+x+3", "1++2", "1", "", "+"},
	"lr_recover_rep": {"1,ab,c", "1,a?b,c", "1,", "1,,a", "1", "a"},
	"lr_looks":       {"1ab", "1az", "1aq", "1q", "(5)ab", "(5", "(x)", "", "1qa"},
	"lr_calls_ops":   {"a;b.c", "a+[b;c]+d.e", "a+", "[a", "a;;b", "a . b", ""},
	"ops_recover":    {"1+2", "1x+2", "1+!2", "(1-~2)", "1 #c# + 2", "1 #c", "~", ""},
	"ops_lines":      {"a1b+c", "(a)*b^c", "(a", "12*3", "02", "a**b", "a^", "a^b^c", ""},
	"recover_nested": {"<abc>", "<a!1b>", "<a!x>", "<a1b2>", "<a##>", "<", "<>"},
	"scopes":         {"a,b,c", "a12,b", "a,z", "#x,b", "#", "a,", ""},
	"lr_inline":      {"1,a,b;2,c+d1e;3,ab", "(1),aq;2;3", "1,az;2,x?y;3", "(1;2;3", "1,a;2,!;3", "1;2+;3", ""},
}

// routeCases returns the route grammars with their inputs.
func routeCases(t *testing.T) []genCase {
	paths, err := filepath.Glob(filepath.Join("testdata", "routes", "*.pego"))
	if err != nil || len(paths) != len(routeInputs) {
		t.Fatalf("route grammars: %d files, want %d: %v", len(paths), len(routeInputs), err)
	}
	var cases []genCase
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(p), ".pego")
		inputs, ok := routeInputs[name]
		if !ok {
			t.Fatalf("no inputs for route grammar %s", name)
		}
		cases = append(cases, genCase{name, string(src), inputs})
	}
	return cases
}

// TestFlowRoutesPrinted checks that every route grammar has rules printed from Flow IR in every
// flowModes mode, and that the modes with inlined bodies print framed bodies from Flow IR.
func TestFlowRoutesPrinted(t *testing.T) {
	frames := 0
	for _, mode := range flowModes {
		for _, gc := range routeCases(t) {
			_, n, f, ok := flowGenerate(gc.src, mode.opts)
			if !ok {
				t.Fatalf("%s %s: rejected", mode.name, gc.name)
			}
			if n == 0 {
				t.Errorf("%s %s: no rule printed from Flow IR", mode.name, gc.name)
			}
			if mode.frames {
				frames += f
			}
		}
	}
	t.Logf("%d framed bodies and lines printed from Flow IR in the route grammars", frames)
	if frames == 0 {
		t.Error("no framed body printed from Flow IR in the route grammars")
	}
}

// TestFlowRoutesPrintFromFlow checks that the typed rules of every route grammar are printed from
// Flow IR in every flowModes mode; the engine comparison of the route grammars is TestFlowRoutesBuild.
func TestFlowRoutesPrintFromFlow(t *testing.T) {
	for _, mode := range flowModes {
		for _, gc := range routeCases(t) {
			_, n, _, ok := flowGenerate(gc.src, mode.opts)
			if !ok {
				t.Fatalf("%s %s: rejected", mode.name, gc.name)
			}
			if n == 0 {
				t.Errorf("%s %s: no rule printed from Flow IR", mode.name, gc.name)
			}
		}
	}
}

// TestFlowRoutesBuild builds the route grammars with dgen and with the default Flow IR printer,
// with and without
// the ordinary work of the typed runtime, and checks that both parse every input the way the
// engine does: the same values, and the same errors as the engine's.
func TestFlowRoutesBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skip("go command not found")
	}
	for _, ordinary := range []string{"", "1"} {
		t.Run("ordinary="+ordinary, func(t *testing.T) {
			t.Setenv("PEGO_TEST_ORDINARY", ordinary)
			testGeneratedTypesCorpus(t, goBin, routeCases(t), GenOptions{convertTypes: true}, 1)
		})
	}
}

// TestFlowRoutesRejected checks that an Operators Rule is accepted only as a whole rule body:
// the source forms that place it elsewhere fail to parse, and bodies spliced in through the
// grammar API, such as a repetition or a predicate around it, make the generator fail.
func TestFlowRoutesRejected(t *testing.T) {
	base := `rule main = a $$
rule a = a "+" "x" / "x"
rule o = operators {
    operand "x"
    level { infix left "+" }
}
`
	wrap := map[string]func(op grammar.Expr) grammar.Expr{
		"leader body": func(op grammar.Expr) grammar.Expr { return op },
		"recover body": func(op grammar.Expr) grammar.Expr {
			return &grammar.Attributed{Expr: op, Attrs: []*grammar.Attribute{{Name: "recover", Pragma: true, KwArgs: map[string]any{"skip": &grammar.Literal{Value: "y"}}}}}
		},
		"repetition": func(op grammar.Expr) grammar.Expr {
			return &grammar.Repeat{Expr: &grammar.Capture{Name: "c", Expr: op}, Min: 0, Max: -1}
		},
		"positive look": func(op grammar.Expr) grammar.Expr {
			return &grammar.Seq{Items: []grammar.Expr{&grammar.And{Expr: op}, &grammar.Literal{Value: "x"}}}
		},
		"negative look": func(op grammar.Expr) grammar.Expr {
			return &grammar.Seq{Items: []grammar.Expr{&grammar.Not{Expr: op}, &grammar.Literal{Value: "x"}}}
		},
		"capture in negative look": func(op grammar.Expr) grammar.Expr {
			return &grammar.Seq{Items: []grammar.Expr{&grammar.Not{Expr: &grammar.Capture{Name: "c", Expr: op}}, &grammar.Literal{Value: "x"}}}
		},
	}
	for _, src := range []string{
		"rule main = a $$\nrule a = a \"+\" (operators { operand \"x\" }) / \"x\"\n",
		"rule main = a $$\nrule a = operators { operand \"x\" } #recover(skip=\"y\")\n",
		"rule main = (operators { operand \"x\" })* $$\n",
		"rule main = &operators { operand \"x\" } \"x\" $$\n",
	} {
		if _, err := parseTest(src); err == nil {
			t.Errorf("parse %q: accepted", src)
		}
	}
	for name, w := range wrap {
		g, err := parseTest(base)
		if err != nil {
			t.Fatal(err)
		}
		var op grammar.Expr
		for _, d := range g.File.Decls {
			if rd, ok := d.(*grammar.RuleDef); ok && rd.Name == "o" {
				op = rd.Expr
			}
		}
		for _, d := range g.File.Decls {
			if rd, ok := d.(*grammar.RuleDef); ok && rd.Name == "a" {
				seq := rd.Expr.(*grammar.Choice).Alts[0].(*grammar.Seq)
				seq.Items[len(seq.Items)-1] = w(op)
			}
		}
		if _, err = generate(g, GenOptions{Types: true, TypedOnly: true, Module: "example.com/p"}); err == nil {
			t.Errorf("generate with an Operators Rule spliced into a %s inside a leader: accepted", name)
		}
	}
}
