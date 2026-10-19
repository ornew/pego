package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// Cancel at a deterministic poll, without timing or global compiler hooks.
type compilePollContext struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *compilePollContext) Err() error {
	c.left--
	if c.left == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func compileTestContext(t *testing.T, polls int) *compilePollContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &compilePollContext{Context: ctx, cancel: cancel, left: polls}
}

func compileTestGrammar(t *testing.T) *grammar.Grammar {
	t.Helper()
	g, err := syntax.Parse(inferredChain(1024, false, ""))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCompileContextCancellation(t *testing.T) {
	g := compileTestGrammar(t)
	before, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	for _, polls := range []int{1, 3, 12} {
		ctx := compileTestContext(t, polls)
		prog, err := CompileContext(ctx, g, Options{})
		if prog != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("polls=%d: program=%v error=%v", polls, prog, err)
		}
	}
	after, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("cancelled compilation mutated source grammar")
	}
}

func TestCompileContextCancelsDuringGrammarValidation(t *testing.T) {
	items := make([]grammar.Expr, 1024)
	for i := range items {
		items[i] = &grammar.Literal{Value: "a"}
	}
	items[len(items)-1] = nil
	g := &grammar.Grammar{Statements: []grammar.Statement{
		&grammar.RuleDef{Name: "main", Expr: &grammar.Seq{Items: items}},
	}}
	if _, err := Compile(g, Options{}); err == nil {
		t.Fatal("fixture must fail ordinary grammar validation")
	}
	// CompileContext and buildControlled make two entry checks, followed by
	// ValidateContext's entry check. The fourth check is within AST traversal.
	ctx := compileTestContext(t, 4)
	prog, err := CompileContext(ctx, g, Options{})
	if prog != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("validation cancellation: program=%v error=%v", prog, err)
	}
	if ctx.left > 0 {
		t.Fatal("compiler did not poll inside grammar validation")
	}
}

func TestCompileContextPhaseCancellation(t *testing.T) {
	g := compileTestGrammar(t)
	p, err := Compile(g, Options{NoTypeCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"static-analysis", "success-proof", "failure-proof", "cut-proof", "dependency-graph", "type-checking", "type-inference", "matcher-construction", "list-constructor-type"} {
		t.Run(phase, func(t *testing.T) {
			ctx := compileTestContext(t, 1)
			control := &compileControl{ctx: ctx}
			defer func() {
				failure := recover()
				cancelled, ok := failure.(compileCancellation)
				if !ok || !errors.Is(cancelled.err, context.Canceled) {
					t.Fatalf("unexpected unwind: %v", failure)
				}
			}()
			switch phase {
			case "static-analysis":
				analyzeControlled(g.Rules(), control)
			case "success-proof", "failure-proof", "cut-proof":
				items := make([]grammar.Expr, 1024)
				for i := range items {
					items[i] = &grammar.Top{}
					if phase == "failure-proof" {
						items[i] = &grammar.Bottom{}
					}
				}
				var expr grammar.Expr = &grammar.Seq{Items: items}
				if phase == "failure-proof" {
					expr = &grammar.Not{Expr: &grammar.Choice{Alts: items}}
				} else if phase == "cut-proof" {
					expr = &grammar.Optional{Expr: expr}
				}
				// Only the shared proof traversal can reach the first context
				// checkpoint; no outer analysis loop contributes polling here.
				a := &analysis{control: control}
				a.alwaysSucceeds(expr)
			case "dependency-graph":
				edges := map[string][]string{}
				for _, rd := range g.Rules() {
					edges[rd.Name] = allCalls(rd.Expr, nil)
				}
				tarjanControlled(g.Rules(), edges, control)
			case "type-checking":
				var diagnostics ErrorList
				checkTypesControlled(p, &diagnostics, control)
			case "type-inference":
				valueTypes(p, control)
			case "list-constructor-type":
				// The entry poll alone cannot cancel: cancellation must be
				// observed inside the temporary checker resolving Elem.
				var elem grammar.TypeExpr = &grammar.TypeRef{Name: "Match"}
				for range 1024 {
					elem = &grammar.ListType{Elem: elem}
				}
				c := &compiler{prog: p, control: control}
				c.checkTerm(&grammar.ListConstructor{Elem: elem}, newScope(), nil)
			case "matcher-construction":
				items := make([]grammar.Expr, 1024)
				for i := range items {
					items[i] = &grammar.Literal{Value: "a"}
				}
				c := &compiler{prog: p, control: control, plainDispatch: true}
				c.expr(&grammar.Seq{Items: items}, newScope(), true)
			}
			t.Fatal("phase completed without cancellation")
		})
	}
}

func TestCompileContextOrdinaryParity(t *testing.T) {
	for _, src := range []string{
		`type P = {}; rule main = "a" => P{}`,
		`rule main = operators { operand [0-9]; level sum { infix left "+" } }`,
		`rule main = "" / "a"`,
		`rule main = ("a" #cut "b") / ""`,
		`rule main = "a" / missing`,
		`type P = { X node[string] }; rule main = "a" => P{X: 1}`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		ordinary, ordinaryErr := Compile(g, Options{})
		ctx, cancel := context.WithCancel(context.Background())
		controlled, controlledErr := CompileContext(ctx, g, Options{})
		cancel()
		if (ordinaryErr == nil) != (controlledErr == nil) {
			t.Fatalf("error mismatch: %v / %v", ordinaryErr, controlledErr)
		}
		if ordinaryErr != nil {
			if ordinaryErr.Error() != controlledErr.Error() {
				t.Fatalf("diagnostic mismatch: %v / %v", ordinaryErr, controlledErr)
			}
			continue
		}
		for _, rd := range g.Rules() {
			x, ok := ordinary.RuleType(rd.Name)
			y, controlledOK := controlled.RuleType(rd.Name)
			if x != y || ok != controlledOK {
				t.Fatalf("type mismatch: %s / %s", x, y)
			}
		}
		// Cancelling the compile context after success must not cancel execution.
		x, ex := ordinary.Parse("main", "a")
		y, ey := controlled.Parse("main", "a")
		if (ex == nil) != (ey == nil) {
			t.Fatalf("parse error mismatch: %v / %v", ex, ey)
		}
		xj, _ := json.Marshal(x)
		yj, _ := json.Marshal(y)
		if !reflect.DeepEqual(xj, yj) {
			t.Fatal("parse value mismatch")
		}
	}
}
