package lsp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/starttest"
	"github.com/ornew/zeroparse/internal/syntax"
)

type lspCompilePollContext struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *lspCompilePollContext) Err() error {
	c.left--
	if c.left == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestCompileCancellationDoesNotPublishDiagnostics(t *testing.T) {
	var source strings.Builder
	source.WriteString("@start\nrule main = r0\n")
	for i := range 1024 {
		fmt.Fprintf(&source, "rule r%d = \"a\"\n", i)
	}
	g, err := syntax.Parse(source.String())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &lspCompilePollContext{Context: ctx, cancel: cancel, left: 4}
	a := newAnalysis(source.String())
	a.g = g
	if err := a.compileContext(controlled); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected compile cancellation, got %v", err)
	}
	if a.prog != nil || len(a.diags) != 0 {
		t.Fatalf("unfinished compilation leaked program or diagnostics: %v, %+v", a.prog, a.diags)
	}
	// A cancellation must not poison the grammar or a subsequent snapshot.
	if err := a.compileContext(context.Background()); err != nil || a.prog == nil || len(a.diags) != 0 {
		t.Fatalf("subsequent ordinary compilation failed: %v, %+v", err, a.diags)
	}
}

func TestGrammarValidationCancellationDoesNotPublishDiagnostics(t *testing.T) {
	items := make([]grammar.Expr, 1024)
	for i := range items {
		items[i] = &grammar.Literal{Value: "a"}
	}
	items[len(items)-1] = nil
	a := newAnalysis(`rule main = "a"`)
	a.g = starttest.MarkAll(&grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{
		&grammar.RuleDef{Name: "main", Expr: &grammar.Seq{Items: items}},
	}}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &lspCompilePollContext{Context: ctx, cancel: cancel, left: 4}
	if err := a.compileContext(controlled); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected validation cancellation, got %v", err)
	}
	if a.prog != nil || len(a.diags) != 0 {
		t.Fatalf("cancelled validation published a result: %v, %+v", a.prog, a.diags)
	}
	items[len(items)-1] = &grammar.Literal{Value: "a"}
	if err := a.compileContext(context.Background()); err != nil || a.prog == nil || len(a.diags) != 0 {
		t.Fatalf("later compilation failed: %v, %+v", err, a.diags)
	}
}
