package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
)

func TestCompileMetadataCollectionCancellation(t *testing.T) {
	g := &grammar.Grammar{}
	a := &grammar.Attribute{Name: "error"}
	for range 1024 {
		g.File.Decls = append(g.File.Decls, &grammar.TypeDef{Name: "T"}, &grammar.RuleDef{Name: "a"})
		a.Args = append(a.Args, "label")
	}
	expr := &grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: []*grammar.Attribute{a}}
	for _, phase := range []struct {
		name string
		run  func(*compile.Control)
	}{
		{"rules", func(c *compile.Control) { c.Rules(g) }},
		{"types", func(c *compile.Control) { c.Types(g) }},
		{"arguments", func(c *compile.Control) { c.ExpressionArgs(a) }},
		{"call-walk", func(c *compile.Control) { compile.AllCallsControlled(expr, nil, c) }},
		{"type-check-arguments", func(c *compile.Control) {
			(&compile.ValueInference{Control: c}).Predicates(expr, &compile.ValueEnv{})
		}},
		{"stream-walk", func(c *compile.Control) { (&compile.Compiler{Control: c}).CheckStream(expr) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &compile.Control{Ctx: compileTestContext(t, 2)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(compile.Cancellation)
				if !ok || !errors.Is(stopped.Err, context.Canceled) {
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
		run  func(*compile.Control)
	}{
		{"candidate-walk", func(c *compile.Control) {
			compile.RuleProjectionsControlled(&compile.Program{}, &compile.Rule{Def: &grammar.RuleDef{Expr: expr}, Action: &grammar.StringLit{}}, c)
		}},
		{"action-use", func(c *compile.Control) { compile.MarkProjectedMaps(term, nil, map[*grammar.Call]bool{}, c) }},
		{"action-indices", func(c *compile.Control) { compile.UsesItems(term, c) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &compile.Control{Ctx: compileTestContext(t, 1)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(compile.Cancellation)
				if !ok || !errors.Is(stopped.Err, context.Canceled) {
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
	control := &compile.Control{Ctx: compileTestContext(t, 1)}
	defer func() {
		failure := recover()
		stopped, ok := failure.(compile.Cancellation)
		if !ok || !errors.Is(stopped.Err, context.Canceled) {
			t.Fatalf("empty attribute rows did not stop: %v", failure)
		}
	}()
	(&compile.ValueInference{Control: control}).Predicates(&grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: attrs}, &compile.ValueEnv{})
}
