package engine

import (
	"fmt"
	"github.com/ornew/pego/grammar"
	"strings"
	"testing"
)

// checks returns how many context checks ctx has seen, for a context from compileTestContext
// created with a budget of 1<<20.
func checks(ctx *compilePollContext) int { return (1 << 20) - ctx.left }

// The shape analysis of a rule body and the reference-cycle analysis of the value types each visit
// every expression and rule. They poll the compilation's context, so a large rule or grammar does
// not run unchecked between the polls of the phases around them.
func TestShapeAnalysisPollsContext(t *testing.T) {
	const items = 4096
	var body strings.Builder
	for i := range items {
		fmt.Fprintf(&body, "c%d:\"a\" ", i)
	}
	g, err := parseTest("rule main = " + body.String())
	if err != nil {
		t.Fatal(err)
	}
	prog, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prog.shapeControl != nil {
		t.Fatal("the compilation's control outlives the compilation")
	}
	expr := prog.rules[0].def.Expr
	want := len(shapeElems(prog, expr))
	if want != items {
		t.Fatalf("shape has %d elements, want %d", want, items)
	}
	ctx := compileTestContext(t, 1<<20)
	prog.shapeControl = &compileControl{ctx: ctx}
	if got := len(shapeElems(prog, expr)); got != want {
		t.Fatalf("shape with a control has %d elements, want %d", got, want)
	}
	if got := checks(ctx); got < items/compilePollStride {
		t.Errorf("the shape analysis of %d items made %d checks, want at least %d", items, got, items/compilePollStride)
	}
	prog.shapeControl = nil
	ctx = compileTestContext(t, 1<<20)
	shapeElems(prog, expr)
	if got := checks(ctx); got != 0 {
		t.Errorf("without a control the shape analysis checked a context %d times", got)
	}
}

func TestRuleCyclesPollsContext(t *testing.T) {
	const rules = 2048
	var src strings.Builder
	for i := range rules {
		fmt.Fprintf(&src, "rule r%d = \"a\" r%d?\n", i, (i+1)%rules)
	}
	g, err := parseTest(src.String())
	if err != nil {
		t.Fatal(err)
	}
	defs := g.Rules()
	byName := map[string]*grammar.RuleDef{}
	for _, rd := range defs {
		byName[rd.Name] = rd
	}
	ctx := compileTestContext(t, 1<<20)
	cycles := ruleCyclesControlled(defs, byName, &compileControl{ctx: ctx})
	if len(cycles) != rules || len(cycles["r0"]) != rules {
		t.Fatalf("cycles for %d rules, the first of %d", len(cycles), len(cycles["r0"]))
	}
	if got := checks(ctx); got < rules/compilePollStride {
		t.Errorf("%d rules made %d checks, want at least %d", rules, got, rules/compilePollStride)
	}
}
