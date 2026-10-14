package lsp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
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
	source.WriteString("rule main = r0\n")
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
