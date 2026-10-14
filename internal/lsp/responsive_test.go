package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type cancelAnalysisOnCheck struct {
	checks   int
	cancelAt int
}

func (*cancelAnalysisOnCheck) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAnalysisOnCheck) Done() <-chan struct{}       { return nil }
func (*cancelAnalysisOnCheck) Value(any) any               { return nil }
func (c *cancelAnalysisOnCheck) Err() error {
	c.checks++
	if c.checks >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestResponsiveAnalysisCancelsWithinToken(t *testing.T) {
	src := "\"" + strings.Repeat("x", 8192) + "\""
	// Pass the analysis preflight, text index, lexer preflight, rune
	// decoding, and lexing-loop check. Cancel at the next poll inside
	// the long string token.
	ctx := &cancelAnalysisOnCheck{cancelAt: 8 + (len(src)-1)/256 + 2*(len([]rune(src))/256)}
	a, err := analyzeContext(ctx, src)
	if a != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("analyzeContext = %v, %v after %d polls", a, err, ctx.checks)
	}
}

func TestResponsiveAnalysisCancelsWithinTextIndex(t *testing.T) {
	src := strings.Repeat("😀\r\n", 4096)
	// Analyze preflight, index preflight and first index poll succeed;
	// the next poll cancels during index construction.
	ctx := &cancelAnalysisOnCheck{cancelAt: 4}
	a, err := analyzeContext(ctx, src)
	if a != nil || !errors.Is(err, context.Canceled) || ctx.checks != ctx.cancelAt {
		t.Fatalf("analyzeContext = %v, %v after %d polls", a, err, ctx.checks)
	}
}

func TestCancellableTextIndexMatchesOrdinary(t *testing.T) {
	src := "😀a\r\nbé\r中\n"
	got, err := newTextIndexContext(context.Background(), src)
	if err != nil || !reflect.DeepEqual(got, newTextIndex(src)) {
		t.Fatalf("text index mismatch: %v", err)
	}
}

func newResponsiveClient(t *testing.T, lint bool) *testClient {
	t.Helper()
	c := newServer(t)
	c.request("initialize", map[string]any{"capabilities": map[string]any{
		"workspace":    map[string]any{"workspaceEdit": map[string]any{"documentChanges": true}},
		"experimental": map[string]any{"pego": map[string]any{"responsiveAnalysis": true, "lintDiagnostics": lint, "showExplanation": true}},
	}})
	return c
}

func asyncRequest(c *testClient, id int, method string, params any) {
	c.t.Helper()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

func assertResponseError(t *testing.T, response map[string]json.RawMessage, code int) {
	t.Helper()
	var got rpcError
	if err := json.Unmarshal(response["error"], &got); err != nil {
		t.Fatalf("response=%v error=%v", response, err)
	}
	if got.Code != code {
		t.Fatalf("response error=%+v want code %d", got, code)
	}
	if _, ok := response["result"]; ok {
		t.Fatalf("error response includes result: %v", response)
	}
}

func awaitBarrier(t *testing.T, barrier <-chan struct{}) {
	t.Helper()
	select {
	case <-barrier:
	case <-timeoutC():
		t.Fatal("worker did not reach controlled barrier")
	}
}

func TestResponsiveRequestCancellationProtocol(t *testing.T) {
	started := make(chan struct{})
	requestHook = func(ctx context.Context, method string) {
		if method == "textDocument/hover" {
			close(started)
			<-ctx.Done()
		}
	}
	t.Cleanup(func() { requestHook = nil })
	c := newResponsiveClient(t, false)
	defer c.exit()
	uri := "file:///cancel.pego"
	c.open(uri, "rule main = \"a\"")
	asyncRequest(c, 100, "textDocument/hover", at(uri, 0, 5))
	awaitBarrier(t, started)
	c.notify("$/cancelRequest", map[string]any{"id": 100})
	assertResponseError(t, c.response("100"), codeRequestCancelled)
	// A subsequent request proves the cancelled worker cannot send a second reply.
	var symbols []DocumentSymbol
	c.requestInto("textDocument/documentSymbol", docParams(uri), &symbols)
	if len(symbols) != 1 {
		t.Fatalf("symbols=%+v", symbols)
	}
}

func TestResponsiveAnalysisCoalescingProtocol(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	analyzed := make(chan string, 3)
	analyzeHook = func(text string) {
		analyzed <- text
		if strings.Contains(text, "first") {
			close(started)
			<-release
		}
	}
	t.Cleanup(func() { analyzeHook = nil })
	c := newResponsiveClient(t, false)
	defer func() { releaseWorker(); c.exit() }()
	uri := "file:///coalesce.pego"
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": "rule first = \"a\""}})
	awaitBarrier(t, started)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": "rule obsolete = \"b\""}}})
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3}, "contentChanges": []any{map[string]any{"text": "rule latest = \"c\""}}})
	// Cancelling a request waiting for analysis proves the actor processed both changes.
	asyncRequest(c, 101, "textDocument/documentSymbol", docParams(uri))
	c.notify("$/cancelRequest", map[string]any{"id": 101})
	assertResponseError(t, c.response("101"), codeRequestCancelled)
	releaseWorker()
	diagnostics := c.diagnostics(uri)
	if diagnostics.Version == nil || *diagnostics.Version != 3 || len(diagnostics.Diagnostics) != 0 {
		t.Fatalf("published obsolete analysis: %+v", diagnostics)
	}
	first, latest := <-analyzed, <-analyzed
	if !strings.Contains(first, "first") || !strings.Contains(latest, "latest") {
		t.Fatalf("analyzed %q then %q", first, latest)
	}
	var symbols []DocumentSymbol
	c.requestInto("textDocument/documentSymbol", docParams(uri), &symbols)
	if len(symbols) != 1 || symbols[0].Name != "latest" {
		t.Fatalf("symbols=%+v", symbols)
	}
}

func TestResponsiveCloseDropsPendingAnalysisProtocol(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	analyzeHook = func(text string) {
		if strings.Contains(text, "blocked") {
			close(started)
			<-release
		}
	}
	t.Cleanup(func() { analyzeHook = nil })
	c := newResponsiveClient(t, false)
	defer func() { releaseWorker(); c.exit() }()
	uri := "file:///closed.pego"
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": "rule blocked = \"a\""}})
	awaitBarrier(t, started)
	c.notify("textDocument/didClose", docParams(uri))
	clear := c.diagnostics(uri)
	if len(clear.Diagnostics) != 0 || clear.Version != nil {
		t.Fatalf("close diagnostics=%+v", clear)
	}
	c.requestError("textDocument/documentSymbol", docParams(uri))
	releaseWorker()
	other := "file:///other.pego"
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": other, "version": 1, "text": "rule main = \"b\""}})
	var published publishDiagnosticsParams
	message := c.next()
	if err := json.Unmarshal(message["params"], &published); err != nil {
		t.Fatal(err)
	}
	if published.URI != other || len(published.Diagnostics) != 0 {
		t.Fatalf("unexpected publication after close: %+v", published)
	}
	// A stale closed-document publication must also not follow the valid result.
	c.request("textDocument/documentSymbol", docParams(other))
	for _, pending := range c.pending {
		if strings.Contains(string(pending["params"]), uri) {
			t.Fatalf("stale closed publication: %v", pending)
		}
	}
}

func TestResponsiveCodeActionContentModifiedProtocol(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	analyzeHook = func(text string) {
		if strings.Contains(text, "lint:ignore") {
			close(started)
			<-release
		}
	}
	t.Cleanup(func() { analyzeHook = nil })
	c := newResponsiveClient(t, true)
	defer func() { releaseWorker(); c.exit() }()
	uri := "file:///stale-actions.pego"
	source := "rule main = (\n  a:\"a\" b:\"b\"\n) => $a"
	diagnostics := c.open(uri, source)
	var target Diagnostic
	for _, d := range diagnostics {
		if d.Code == "unused-capture" {
			target = d
			break
		}
	}
	if target.Data == nil {
		t.Fatalf("missing lint diagnostic: %+v", diagnostics)
	}
	asyncRequest(c, 102, "textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri}, "range": target.Range, "context": map[string]any{"diagnostics": []Diagnostic{target}}})
	awaitBarrier(t, started)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": strings.Replace(source, "b:\"b\"", "b:\"c\"", 1)}}})
	assertResponseError(t, c.response("102"), codeContentModified)
	releaseWorker()
	current := c.diagnostics(uri)
	if current.Version == nil || *current.Version != 2 {
		t.Fatalf("version=%+v", current)
	}
	c.request("textDocument/documentSymbol", docParams(uri))
}

func TestResponsiveFailedAnalysisDropsLintProtocol(t *testing.T) {
	analyzeHook = func(text string) {
		if strings.Contains(text, "BOOM") {
			panic("boom")
		}
	}
	t.Cleanup(func() { analyzeHook = nil })
	c := newResponsiveClient(t, true)
	defer c.exit()
	uri := "file:///failed-lint.pego"
	first := c.open(uri, "rule main = \"a\" / \"ab\"")
	if len(first) == 0 || first[0].Source != "pego-lint" {
		t.Fatalf("lint diagnostics=%+v", first)
	}
	current := change(c, uri, 2, map[string]any{"text": "rule BOOM = \"a\""})
	if len(current) != 1 || current[0].Source != "pego" || !strings.Contains(current[0].Message, "boom") {
		t.Fatalf("stale lint after failure: %+v", current)
	}
	if err := c.requestError("textDocument/documentSymbol", docParams(uri)); err.Code != codeContentModified {
		t.Fatalf("old ranges reused: %+v", err)
	}
}

func TestResponsiveShutdownCancelsRequestProtocol(t *testing.T) {
	started := make(chan struct{})
	requestHook = func(ctx context.Context, method string) {
		if method == "textDocument/hover" {
			close(started)
			<-ctx.Done()
		}
	}
	t.Cleanup(func() { requestHook = nil })
	c := newResponsiveClient(t, false)
	uri := "file:///shutdown.pego"
	c.open(uri, "rule main = \"a\"")
	asyncRequest(c, 103, "textDocument/hover", at(uri, 0, 5))
	awaitBarrier(t, started)
	asyncRequest(c, 104, "shutdown", nil)
	assertResponseError(t, c.response("103"), codeRequestCancelled)
	if response := c.response("104"); len(response["error"]) > 0 {
		t.Fatalf("shutdown=%v", response)
	}
	c.notify("exit", nil)
	c.wait(nil)
}

func TestResponsiveSaveRejectsOldFindingGenerationProtocol(t *testing.T) {
	c := newResponsiveClient(t, true)
	defer c.exit()
	uri := "file:///save-generation.pego"
	source := "rule main = (\n a:\"a\" b:\"b\"\n) => $a"
	diagnostics := c.open(uri, source)
	var target Diagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "unused-capture" {
			target = diagnostic
			break
		}
	}
	if target.Data == nil || target.Data.Generation == 0 {
		t.Fatalf("finding has no generation: %+v", diagnostics)
	}
	c.notify("textDocument/didSave", docParams(uri))
	current := c.diagnostics(uri)
	if current.Version == nil || *current.Version != target.Data.Version {
		t.Fatalf("unexpected save version: %+v", current)
	}
	var actions []codeAction
	c.requestInto("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri}, "range": target.Range, "context": map[string]any{"diagnostics": []Diagnostic{target}}}, &actions)
	if len(actions) != 0 {
		t.Fatalf("old generation actions: %+v", actions)
	}
}
