package compile

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
		g.File.Decls = append(g.File.Decls, &grammar.TypeDef{Name: "T"}, &grammar.RuleDef{Name: "a"})
		a.Args = append(a.Args, "label")
	}
	expr := &grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: []*grammar.Attribute{a}}
	for _, phase := range []struct {
		name string
		run  func(*Control)
	}{
		{"rules", func(c *Control) { c.rules(g) }},
		{"types", func(c *Control) { c.types(g) }},
		{"arguments", func(c *Control) { c.expressionArgs(a) }},
		{"call-walk", func(c *Control) { allCallsControlled(expr, nil, c) }},
		{"type-check-arguments", func(c *Control) {
			(&ValueInference{control: c}).predicates(expr, &valueEnv{})
		}},
		{"stream-walk", func(c *Control) { (&Compiler{Control: c}).checkStream(expr) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &Control{Ctx: compileTestContext(t, 2)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(Cancellation)
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
		run  func(*Control)
	}{
		{"candidate-walk", func(c *Control) {
			ruleProjectionsControlled(&Program{}, &Rule{Def: &grammar.RuleDef{Expr: expr}, Action: &grammar.StringLit{}}, c)
		}},
		{"action-use", func(c *Control) { markProjectedMaps(term, nil, map[*grammar.Call]bool{}, c) }},
		{"action-indices", func(c *Control) { usesItems(term, c) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &Control{Ctx: compileTestContext(t, 1)}
			defer func() {
				failure := recover()
				stopped, ok := failure.(Cancellation)
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
	control := &Control{Ctx: compileTestContext(t, 1)}
	defer func() {
		failure := recover()
		stopped, ok := failure.(Cancellation)
		if !ok || !errors.Is(stopped.Err, context.Canceled) {
			t.Fatalf("empty attribute rows did not stop: %v", failure)
		}
	}()
	(&ValueInference{control: control}).predicates(&grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: attrs}, &valueEnv{})
}
