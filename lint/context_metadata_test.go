package lint

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestLintGroupUnwrapPhaseCancellation(t *testing.T) {
	leaf := &grammar.Literal{Value: "a"}
	var expr grammar.Expr = leaf
	for range 1024 {
		expr = &grammar.Group{Expr: expr}
	}
	before := expr.(*grammar.Group).Expr
	control := &lintControl{ctx: lintTestContext(t, 1)}
	defer func() {
		failure := recover()
		cancelled, ok := failure.(lintCancellation)
		if !ok || !errors.Is(cancelled.err, context.Canceled) {
			t.Fatalf("group unwrapping did not stop during left-call analysis: %v", failure)
		}
		if expr.(*grammar.Group).Expr != before || leaf.Value != "a" {
			t.Fatal("cancellation changed the caller's expression")
		}
	}()
	(&analysis{control: control}).leftCalls(expr, nil)
}

func TestLintMetadataPhaseCancellation(t *testing.T) {
	leaf := &grammar.Literal{Value: "a"}
	var groups, wrappers, captures, sequences, optionals, repeats grammar.Expr = leaf, leaf, leaf, leaf, leaf, leaf
	var typ grammar.TypeExpr = &grammar.TypeRef{Name: "Match"}
	elems := make([]grammar.Expr, 1024)
	attrs := make([]*grammar.Attribute, len(elems))
	args := make([]grammar.Term, len(elems))
	operands := make([]*grammar.OperatorsRuleOperand, len(elems))
	for i := range elems {
		groups = &grammar.Group{Expr: groups}
		wrappers = &grammar.Atomic{Expr: &grammar.Discard{Expr: wrappers}}
		captures = &grammar.Capture{Name: "a", Expr: captures}
		sequences = &grammar.Seq{Items: []grammar.Expr{sequences}}
		optionals = &grammar.Optional{Expr: optionals}
		repeats = &grammar.Repeat{Expr: repeats, Min: 1, Max: 1}
		typ = &grammar.TypeGroup{Type: typ}
		elems[i] = leaf
		attrs[i] = &grammar.Attribute{Name: "error"}
		args[i] = &grammar.IntLit{Value: i}
		operands[i] = &grammar.OperatorsRuleOperand{Expr: leaf, Action: args[i]}
	}
	wide := &grammar.Seq{Items: elems}
	term := &grammar.Call{Func: "list", Args: args}
	for _, phase := range []struct {
		name string
		run  func(*lintControl)
	}{
		{"groups", func(c *lintControl) { analysisExpr(groups, c) }},
		{"wrappers", func(c *lintControl) { neutral(wrappers, true, c) }},
		{"sequence-items", func(c *lintControl) { items(groups, true, c) }},
		{"attributes", func(c *lintControl) { hasAttr(&grammar.Attributed{Expr: leaf, Attrs: attrs}, "recover", c) }},
		{"position", func(c *lintControl) { posOfControlled(sequences, c) }},
		{"visibility", func(c *lintControl) { visible(captures, c) }},
		{"comparison-depth", func(c *lintControl) { same(optionals, optionals, false, c) }},
		{"comparison-width", func(c *lintControl) { sameList(elems, elems, false, c) }},
		{"predicate-search", func(c *lintControl) { hasPredicate(wide, c) }},
		{"capture-search", func(c *lintControl) { hasCaptures(wide, c) }},
		{"children", func(c *lintControl) { forChildren(wide, func(grammar.Expr) {}, c) }},
		{"index-search", func(c *lintControl) { usesIndex(term, c) }},
		{"position-terms", func(c *lintControl) { positionRead(&grammar.RuleDef{Expr: leaf, Action: term}, c) }},
		{"position-operands", func(c *lintControl) {
			positionRead(&grammar.RuleDef{Expr: &grammar.OperatorsRule{Operands: operands}}, c)
		}},
		{"repetition-search", func(c *lintControl) { anyRepeat(wide, c) }},
		{"any-character-search", func(c *lintControl) { consumesAny(repeats, c) }},
		{"type-groups", func(c *lintControl) { ungroupType(typ, c) }},
	} {
		t.Run(phase.name, func(t *testing.T) {
			control := &lintControl{ctx: lintTestContext(t, 1)}
			defer func() {
				failure := recover()
				cancelled, ok := failure.(lintCancellation)
				if !ok || !errors.Is(cancelled.err, context.Canceled) {
					t.Fatalf("metadata phase completed without cancellation: %v", failure)
				}
			}()
			phase.run(control)
		})
	}
}

func TestLintMetadataFacts(t *testing.T) {
	control := &lintControl{ctx: lintTestContext(t, 0)}
	pos := grammar.Pos{Line: 4, Col: 7}
	leaf := &grammar.Literal{SourceInfo: grammar.SourceInfo{Pos: pos}, Value: "a"}
	capture := &grammar.Capture{Name: "a", Expr: leaf}
	wrapped := &grammar.Group{Expr: &grammar.Atomic{Expr: &grammar.Discard{Expr: capture}}}
	if got := neutral(wrapped, true, control); got != leaf {
		t.Fatalf("neutral expression = %T, want original literal", got)
	}
	if got := neutral(wrapped, false, control); got != capture {
		t.Fatalf("capture boundary = %T, want original capture", got)
	}
	recovery := &grammar.Recovery{Expr: leaf, Skip: &grammar.Any{}}
	if got, ok := neutral(recovery, true, control).(*grammar.Attributed); !ok || got.Expr != leaf || got.Attrs[0].KwArgs["skip"] != recovery.Skip {
		t.Fatalf("recovery boundary or skip expression lost: %#v", got)
	}
	if got := posOfControlled(&grammar.Seq{Items: []grammar.Expr{&grammar.Group{Expr: leaf}}}, control); got != pos {
		t.Fatalf("position = %v, want %v", got, pos)
	}
	if visible(&grammar.Discard{Expr: leaf}, control) || !visible(capture, control) {
		t.Fatal("metadata changed capture/discard visibility")
	}
	other := &grammar.Capture{Name: "b", Expr: leaf}
	if same(capture, other, false, control) || !same(capture, other, true, control) {
		t.Fatal("comparison did not preserve capture-name policy")
	}
	if same(&grammar.Ref{Name: "a", Level: "first"}, &grammar.Ref{Name: "a", Level: "last"}, true, control) {
		t.Fatal("comparison ignored the selected operator level")
	}
	if !consumesAny(&grammar.Repeat{Expr: &grammar.Any{}, Min: 1, Max: -1}, control) {
		t.Fatal("any-character repetition was not recognized")
	}
}

func TestRunContextMetadataParity(t *testing.T) {
	for _, source := range []string{
		`rule main = (a:("a" #atomic) b:"b") => $a`,
		`rule main = (("a" / "ab") / ("b" / "bc"))`,
		`rule main = &(. #repeat(1,)) "a"`,
		`rule main = "a"` + strings.Repeat(` @error("label")`, 64),
	} {
		g := lintContextGrammar(t, source)
		before, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		want := runUnchecked(g, "main", options{})
		ctx := lintTestContext(t, 0)
		got, err := runContext(ctx, g, "main", options{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("metadata changed findings: %v / %v, %v", got, want, err)
		}
		for _, cancelAt := range []int{1, ctx.calls - 1} {
			findings, err := runContext(lintTestContext(t, cancelAt), g, "main", options{})
			if findings != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("metadata returned partial findings at %d: %v / %v", cancelAt, findings, err)
			}
		}
		after, err := json.Marshal(g)
		if err != nil || string(before) != string(after) {
			t.Fatal("metadata changed caller-owned grammar data")
		}
	}
}
