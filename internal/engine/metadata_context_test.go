package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestCompileMetadataCollectionCancellation(t *testing.T) {
	g := &grammar.Grammar{}
	a := &grammar.Attribute{Name: "error"}
	for range 1024 {
		g.Statements = append(g.Statements, &grammar.TypeDef{Name: "T"}, &grammar.RuleDef{Name: "a"})
		a.Args = append(a.Args, "label")
	}
	expr := &grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: []*grammar.Attribute{a}}
	for _, phase := range []struct {
		name string
		run  func(*compileControl)
	}{
		{"rules", func(c *compileControl) { c.rules(g) }},
		{"types", func(c *compileControl) { c.types(g) }},
		{"arguments", func(c *compileControl) { c.expressionArgs(a) }},
		{"call-walk", func(c *compileControl) { allCallsControlled(expr, nil, c) }},
		{"type-check-arguments", func(c *compileControl) { (&checker{control: c}).attributed(expr, &scopeCheck{caps: caps{}}) }},
		{"stream-walk", func(c *compileControl) { (&compiler{control: c}).checkStream(expr) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &compileControl{ctx: compileTestContext(t, 2)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(compileCancellation)
				if !ok || !errors.Is(stopped.err, context.Canceled) {
					t.Fatalf("collection did not unwind as compilation cancellation: %v", failure)
				}
			}()
			phase.run(control)
		})
	}
}

func TestProjectionMetadataCancellation(t *testing.T) {
	var expr grammar.Expr = &grammar.Literal{Value: "a"}
	var term grammar.Term = &grammar.StringLit{Value: "a"}
	for range 1024 {
		expr = &grammar.Optional{Expr: expr}
		term = &grammar.Member{Name: "field", X: term}
	}
	for _, phase := range []struct {
		name string
		run  func(*compileControl)
	}{
		{"candidate-walk", func(c *compileControl) {
			ruleProjectionsControlled(&Program{}, &rule{def: &grammar.RuleDef{Expr: expr}, action: &grammar.StringLit{}}, c)
		}},
		{"action-use", func(c *compileControl) { markProjectedMapsControlled(term, nil, map[*grammar.Call]bool{}, c) }},
		{"action-indices", func(c *compileControl) { usesItemsControlled(term, c) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &compileControl{ctx: compileTestContext(t, 1)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(compileCancellation)
				if !ok || !errors.Is(stopped.err, context.Canceled) {
					t.Fatalf("projection metadata did not stop: %v", failure)
				}
			}()
			phase.run(control)
		})
	}
}

func TestTypeCheckEmptyAttributeCancellation(t *testing.T) {
	attrs := make([]*grammar.Attribute, 1024)
	for i := range attrs {
		attrs[i] = &grammar.Attribute{Name: "error"}
	}
	control := &compileControl{ctx: compileTestContext(t, 1)}
	defer func() {
		failure := recover()
		stopped, ok := failure.(compileCancellation)
		if !ok || !errors.Is(stopped.err, context.Canceled) {
			t.Fatalf("empty attribute rows did not stop: %v", failure)
		}
	}()
	(&checker{control: control}).attributed(&grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: attrs}, &scopeCheck{caps: caps{}})
}
