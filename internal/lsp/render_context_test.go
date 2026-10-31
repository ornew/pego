package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/zeroparse/internal/syntax"
)

func renderFixture(fields int) string {
	var source strings.Builder
	source.WriteString("// __A type__ with `fields`.\ntype T = {\n")
	for i := range fields {
		fmt.Fprintf(&source, " F%d node[string]\n", i)
	}
	source.WriteString("}\n// The main rule.\n@start\nrule main = ")
	for i := range fields {
		fmt.Fprintf(&source, "c%d:\"a\" ", i)
	}
	source.WriteString("\nrule value = \"v\" => T{}\n")
	return source.String()
}
func renderServer(a *analysis, ctx context.Context, hierarchical, markdown, snippets bool) *Server {
	s := &Server{docs: map[string]*document{"file:///render.zp": {uri: "file:///render.zp", text: a.text, an: a}}, state: stateInitialized, requestContext: ctx}
	s.client.TextDocument.DocumentSymbol.HierarchicalDocumentSymbolSupport = hierarchical
	s.client.TextDocument.Completion.CompletionItem.SnippetSupport = snippets
	if markdown {
		s.client.TextDocument.Hover.ContentFormat = []string{"markdown"}
		s.client.TextDocument.Completion.CompletionItem.DocumentationFormat = []string{"markdown"}
	}
	return s
}
func renderParams(a *analysis, method string) json.RawMessage {
	p := docParams("file:///render.zp")
	if method != "textDocument/documentSymbol" {
		off := strings.Index(a.text, "main =")
		if method == "textDocument/completion" {
			off = strings.Index(a.text, "T{}") + 2
		}
		p["position"] = a.idx.position(off)
	}
	data, _ := json.Marshal(p)
	return data
}
func TestRenderRequestsContextParity(t *testing.T) {
	a := analyze(renderFixture(32))
	if a.prog == nil {
		t.Fatal(a.diags)
	}
	before, _ := json.Marshal(a.g)
	for _, method := range []string{"textDocument/documentSymbol", "textDocument/hover", "textDocument/completion"} {
		for _, rich := range []bool{false, true} {
			params := renderParams(a, method)
			ordinary := renderServer(a, nil, rich, rich, rich)
			msg := &incoming{Method: method, Params: params}
			want, werr := ordinary.request(msg)
			if werr != nil {
				t.Fatal(werr)
			}
			wantJSON, _ := json.Marshal(want)
			for _, ctx := range []context.Context{context.Background(), formatContext(t, 0)} {
				got, err := renderServer(a, ctx, rich, rich, rich).request(msg)
				gotJSON, _ := json.Marshal(got)
				if err != nil || string(gotJSON) != string(wantJSON) {
					t.Fatalf("%s rich=%v: %s/%+v want %s", method, rich, gotJSON, err, wantJSON)
				}
			}
		}
	}
	after, _ := json.Marshal(a.g)
	if string(after) != string(before) {
		t.Fatal("rendering changed the shared grammar")
	}
}
func TestRenderRequestsCancellationDiscardsResponses(t *testing.T) {
	a := analyze(renderFixture(512))
	if a.prog == nil {
		t.Fatal(a.diags)
	}
	for _, method := range []string{"textDocument/documentSymbol", "textDocument/hover", "textDocument/completion"} {
		for _, rich := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", method, rich), func(t *testing.T) {
				msg := &incoming{Method: method, Params: renderParams(a, method)}
				count := formatContext(t, 0)
				if _, err := renderServer(a, count, rich, rich, rich).request(msg); err != nil || count.checks < 4 {
					t.Fatalf("insufficient fixture checks=%d error=%+v", count.checks, err)
				}
				// Sorting may visit keys in different orders; cancel well within the
				// operation rather than relying on a previous invocation's last check.
				for _, at := range []int{1, 2, count.checks / 2} {
					got, err := renderServer(a, formatContext(t, at), rich, rich, rich).request(msg)
					if got != nil || err == nil || err.Code != codeRequestCancelled {
						t.Fatalf("check%d: partial result %T / %+v", at, got, err)
					}
				}
			})
		}
	}
}

func TestRenderInnerWalksCancellation(t *testing.T) {
	a := &analysis{toks: make([]token, 1024)}
	for i := range a.toks {
		a.toks[i].Kind = syntax.TokenComment
	}
	keys := make([]string, 1024)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", 1024-i)
	}
	long := strings.Repeat("x", 16384)
	phases := map[string]func(*renderControl){
		"forward-comments":     func(c *renderControl) { a.significantTokenControlled(0, c) },
		"backward-comments":    func(c *renderControl) { a.previousSignificantTokenControlled(len(a.toks)-1, c) },
		"token-termination":    func(c *renderControl) { terminatedControlled(long, '"', 0, c) },
		"identifier":           func(c *renderControl) { c.isIdentifier(long) },
		"sort":                 func(c *renderControl) { c.sortKeys(keys) },
		"documentation-split":  func(c *renderControl) { c.split(long, "\n") },
		"documentation-output": func(c *renderControl) { c.join([]string{long, long}, "\n") },
		"documentation-trim":   func(c *renderControl) { c.trimSpace(strings.Repeat(" ", 16384)) },
	}
	for name, phase := range phases {
		t.Run(name, func(t *testing.T) {
			completed := false
			got, err := renderRequest(formatContext(t, 2), func(c *renderControl) (any, *rpcError) {
				phase(c)
				completed = true
				return "partial", nil
			})
			if completed || got != nil || err == nil || err.Code != codeRequestCancelled {
				t.Fatalf("walk completed=%v: %v/%+v", completed, got, err)
			}
		})
	}
}
func TestRenderDocumentationAndIdentifierParity(t *testing.T) {
	for _, s := range []string{"", "rule", "_reserved", "x9_", "x_😀", "\xff", strings.Repeat("a", 4097)} {
		c := &renderControl{ctx: formatContext(t, 0)}
		if got := c.isIdentifier(s); got != syntax.IsIdentifier(s) {
			t.Fatalf("identifier %q: %v", s, got)
		}
	}
	texts := []string{"", "a\n", "```zp\nrule a = \"*`*\"\n```\n__doc__ and _em_ [link](target)", strings.Repeat("`word` and __bold__\n", 512), "\xff\u2003"}
	for _, s := range texts {
		c := &renderControl{ctx: formatContext(t, 0)}
		if got := plainDocumentationControlled(s, c); got != plainDocumentation(s) {
			t.Fatalf("documentation mismatch for %d bytes", len(s))
		}
		for _, sep := range []string{"\n", "`"} {
			if got := c.split(s, sep); !reflect.DeepEqual(got, strings.Split(s, sep)) {
				t.Fatal("split mismatch")
			}
			parts := strings.Split(s, sep)
			if c.join(parts, sep) != strings.Join(parts, sep) {
				t.Fatal("join mismatch")
			}
		}
		if c.trimSpace(s) != strings.TrimSpace(s) {
			t.Fatal("trim mismatch")
		}
	}
}
func TestRenderFinalCheckAndForeignPanic(t *testing.T) {
	got, err := renderRequest(formatContext(t, 2), func(*renderControl) (any, *rpcError) { return "ready", nil })
	if got != nil || err == nil || err.Code != codeRequestCancelled {
		t.Fatalf("final check=%v/%+v", got, err)
	}
	marker := &struct{}{}
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("foreign panic changed: %v", got)
		}
	}()
	renderRequest(formatContext(t, 0), func(*renderControl) (any, *rpcError) { panic(marker) })
}
func TestRenderSharedAnalysis(t *testing.T) {
	a := analyze(renderFixture(32))
	if a.prog == nil {
		t.Fatal(a.diags)
	}
	var wg sync.WaitGroup
	for _, method := range []string{"textDocument/documentSymbol", "textDocument/hover", "textDocument/completion"} {
		msg := &incoming{Method: method, Params: renderParams(a, method)}
		for range 4 {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			wg.Go(func() {
				if _, err := renderServer(a, ctx, true, true, true).request(msg); err != nil {
					t.Errorf("shared analysis: %+v", err)
				}
			})
		}
	}
	wg.Wait()
}
