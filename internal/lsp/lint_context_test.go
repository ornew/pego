package lsp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/lint"
)

type lspLintPollContext struct {
	context.Context
	cancel          context.CancelFunc
	calls, cancelAt int
}

func (c *lspLintPollContext) Err() error {
	c.calls++
	if c.cancelAt > 0 && c.calls == c.cancelAt {
		c.cancel()
	}
	return c.Context.Err()
}

func lspLintContext(t *testing.T, cancelAt int) *lspLintPollContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &lspLintPollContext{Context: ctx, cancel: cancel, cancelAt: cancelAt}
}

func TestCancelledLintDoesNotCommitCache(t *testing.T) {
	var source strings.Builder
	source.WriteString("@start\nrule main = \"a\"\n")
	for i := range 512 {
		fmt.Fprintf(&source, "rule unused%d = \"b\"\n", i)
	}
	a := analyze(source.String())
	if a.prog == nil || len(a.diags) != 0 {
		t.Fatalf("invalid fixture: %+v", a.diags)
	}
	count := lspLintContext(t, 0)
	complete, err := a.lintFindingsContext(count)
	if err != nil || len(complete) < 256 {
		t.Fatalf("inadequate fixture: %d findings, %v", len(complete), err)
	}
	if err := a.cacheLintContext(lspLintContext(t, count.calls-1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if a.lintCached || a.lintCache != nil || len(a.diags) != 0 {
		t.Fatal("cancelled lint committed partial results")
	}
	if err := a.cacheLintContext(context.Background()); err != nil || !a.lintCached || len(a.lintCache) != len(complete) {
		t.Fatalf("subsequent lint failed: %v", err)
	}
}

func TestCancelledLintMappingReturnsNoDiagnostics(t *testing.T) {
	a := analyze("@start\nrule main = \"a\"")
	a.lintCached = true
	a.lintCache = make([]lint.Finding, 512)
	for i := range a.lintCache {
		a.lintCache[i] = lint.Finding{Check: lint.CheckUnreachableRule, Pos: a.g.Rules()[0].Diagnostic(), Message: fmt.Sprint(i), Severity: lint.Warning}
	}
	d := &document{uri: "file:///lint-cancel.zp", text: a.text, an: a, version: 1, generation: 1}
	// Finding-cache check, index zero, then index 256: some diagnostics were
	// constructed internally before cancellation, but none may escape.
	diagnostics, err := d.lintDiagnosticsContext(lspLintContext(t, 3))
	if diagnostics != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("partial diagnostics escaped: %+v, %v", diagnostics, err)
	}
	if got := d.lintDiagnostics(); len(got) != 512 {
		t.Fatalf("mapping cancellation poisoned cache: %d findings", len(got))
	}
}

func TestResponsiveLintMappingCancellationProtocol(t *testing.T) {
	const uri = "file:///lint-mapping.zp"
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	releaseWorker := func() { once.Do(func() { close(release) }) }
	lintDiagnosticHook = func(ctx context.Context, snapshot analysisSnapshot, index int) {
		if snapshot.URI != uri || snapshot.Version != 1 || index != 256 {
			return
		}
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
		case <-release:
			return // Cleanup also releases a regressed synchronous actor.
		}
		// Keep the old worker parked after cancellation, so the latest analysis
		// cannot finish before the waiting request cancellation is observed.
		<-release
	}
	t.Cleanup(func() { lintDiagnosticHook = nil })
	c := newResponsiveClient(t, true)
	defer func() { releaseWorker(); c.exit() }()
	var source strings.Builder
	source.WriteString("@start\nrule main = \"a\"\n")
	for i := range 512 {
		fmt.Fprintf(&source, "rule unused%d = \"b\"\n", i)
	}
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": source.String()}})
	awaitBarrier(t, started)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": "@start\nrule main = \"b\""}}})
	awaitBarrier(t, cancelled)
	asyncRequest(c, 701, "textDocument/documentSymbol", docParams(uri))
	c.notify("$/cancelRequest", map[string]any{"id": 701})
	// The actor must process change and cancellation while mapping is blocked.
	assertResponseError(t, c.response("701"), codeRequestCancelled)
	releaseWorker()
	diagnostics := c.diagnostics(uri)
	if diagnostics.Version == nil || *diagnostics.Version != 2 || len(diagnostics.Diagnostics) != 0 {
		t.Fatalf("obsolete/partial lint publication: %+v", diagnostics)
	}
	// The former version's 512 warnings must never precede this current result.
	var symbols []DocumentSymbol
	c.requestInto("textDocument/documentSymbol", docParams(uri), &symbols)
	if len(symbols) != 1 || symbols[0].Name != "main" {
		t.Fatalf("latest analysis unavailable: %+v", symbols)
	}
}
