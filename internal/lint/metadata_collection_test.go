package lint

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestLintMetadataCollectionCancellation(t *testing.T) {
	g := &grammar.Grammar{EndBreak: &grammar.LineBreak{}}
	a := &grammar.Attribute{Name: "error"}
	var rules []*grammar.RuleDef
	for range 1024 {
		r := &grammar.RuleDef{Name: "a", Expr: &grammar.Literal{Value: "a"}}
		rules = append(rules, r)
		g.Statements = append(g.Statements, &grammar.TypeDef{Name: "T"}, r)
		g.EndBreak.Comments = append(g.EndBreak.Comments, &grammar.Comment{})
		a.Args = append(a.Args, "label")
	}
	expr := &grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: []*grammar.Attribute{a}}
	for _, phase := range []struct {
		name     string
		run      func(*lintControl)
		cancelAt int
	}{
		{"rules", func(c *lintControl) { c.rules(g) }, 2},
		{"types", func(c *lintControl) { c.types(g) }, 2},
		{"comments", func(c *lintControl) { c.comments(g) }, 2},
		{"arguments", func(c *lintControl) { c.expressionArgs(a) }, 2},
		{"call-walk", func(c *lintControl) { callsControlled(expr, c) }, 2},
		{"unused-capture-rules", func(c *lintControl) { (&linter{control: c, an: &analysis{g: g, order: rules}}).unusedCaptures() }, 2},
		// With no statements, two Err calls finish rule enumeration; the next
		// collection must stop inside the large comment copy, before reporting.
		{"suppression-comments", func(c *lintControl) { (&linter{control: c}).suppress(&grammar.Grammar{EndBreak: g.EndBreak}) }, 4},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &lintControl{ctx: lintTestContext(t, phase.cancelAt)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(lintCancellation)
				if !ok || !errors.Is(stopped.err, context.Canceled) {
					t.Fatalf("collection did not unwind as lint cancellation: %v", failure)
				}
			}()
			phase.run(control)
		})
	}
}
