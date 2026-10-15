package lsp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestSemanticResponsesControlledParity(t *testing.T) {
	for _, fixture := range semanticResponseSources(t) {
		t.Run(fixture.name, func(t *testing.T) {
			a := analyze(fixture.text)
			beforeTokens := append([]token(nil), a.toks...)
			beforeOccurrences := append([]occurrence(nil), a.occs...)
			value, err := semanticServer(a, nil).request(semanticMessage())
			want := occurrenceResponseJSON(t, value, err)
			for _, ctx := range []context.Context{context.Background(), formatContext(t, 0)} {
				got, gerr := semanticServer(a, ctx).request(semanticMessage())
				if string(occurrenceResponseJSON(t, got, gerr)) != string(want) {
					t.Fatalf("controlled response differs: %v/%+v want %s", got, gerr, want)
				}
			}
			if !reflect.DeepEqual(beforeTokens, a.toks) || !reflect.DeepEqual(beforeOccurrences, a.occs) {
				t.Fatal("request changed shared token or occurrence slices")
			}
			if fixture.name == "empty" && string(want) != `{"Result":{"data":[]},"Error":null}` {
				t.Fatalf("empty result lost its array: %s", want)
			}
		})
	}
}

func TestSemanticControlledCommentEncoding(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		a := analyze("/*😀" + newline + newline + "second" + newline + "*/")
		value, err := semanticServer(a, formatContext(t, 0)).request(semanticMessage())
		if err != nil {
			t.Fatal(err)
		}
		data := value.(map[string]any)["data"].([]int)
		// Empty lines contribute to the next line delta, never a zero-length token.
		want := []int{0, 0, 4, int(semComment), 0, 2, 0, 6, int(semComment), 0, 1, 0, 2, int(semComment), 0}
		if !reflect.DeepEqual(data, want) {
			t.Fatalf("newline %q: %v want %v", newline, data, want)
		}
	}
}

func TestSemanticRequestsCancelAndRetry(t *testing.T) {
	for _, fixture := range []semanticSourceCase{
		{"dense", semanticDenseSource(4096)},
		{"multiline", "/*😀\r\n" + strings.Repeat("content😀\r\n\r\n", 2048) + "*/"},
		{"operators", semanticOperatorsSource(128)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			a := analyze(fixture.text)
			msg := semanticMessage()
			value, err := semanticServer(a, nil).request(msg)
			want := occurrenceResponseJSON(t, value, err)
			count := formatContext(t, 0)
			if _, err := semanticServer(a, count).request(msg); err != nil || count.checks < 8 {
				t.Fatalf("checks=%d err=%+v", count.checks, err)
			}
			for _, at := range []int{1, 2, count.checks / 2, count.checks} {
				got, gerr := semanticServer(a, formatContext(t, at)).request(msg)
				if got != nil || gerr == nil || gerr.Code != codeRequestCancelled {
					t.Fatalf("check%d returned %T/%+v", at, got, gerr)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, gerr := semanticServer(a, ctx).request(msg)
			if string(occurrenceResponseJSON(t, got, gerr)) != string(want) {
				t.Fatal("retry response changed")
			}
		})
	}
}

func semanticLastIdent(t *testing.T, a *analysis, name string) int {
	t.Helper()
	for i := len(a.toks) - 1; i >= 0; i-- {
		if a.isIdent(i, name) {
			return i
		}
	}
	t.Fatalf("missing %q token", name)
	return -1
}

func semanticBodyPrefixChecks(t *testing.T, a *analysis, i int, includeHeader bool) int {
	t.Helper()
	count := formatContext(t, 0)
	c := &renderControl{ctx: count}
	c.poll() // inOperatorsBody entry, excluding the renderRequest preflight
	st := i
	for st >= 0 && !a.isDefKeyword(st) {
		c.poll()
		st--
	}
	if !a.isIdent(st, "rule") {
		t.Fatal("missing rule header")
	}
	if includeHeader && a.ruleBodyStartControlled(st, i, c) < 0 {
		t.Fatal("missing rule body")
	}
	return count.checks
}

func TestSemanticInteriorWalksCancel(t *testing.T) {
	t.Run("multiline", func(t *testing.T) {
		a := analyze("/*😀\n" + strings.Repeat("line\n\n", 2048) + "*/")
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) { return a.semanticTokensControlled(c), nil })
	})
	t.Run("occurrence-cursor", func(t *testing.T) {
		a := &analysis{toks: []token{{Token: syntax.Token{Kind: syntax.TokenIdent, Text: "tail"}, start: 8192, end: 8196}}}
		for i := range 4096 {
			a.occs = append(a.occs, occurrence{start: i * 2, end: i*2 + 1})
		}
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) { return a.semanticTokensControlled(c), nil })
	})
	t.Run("backward-body", func(t *testing.T) {
		a := analyze(strings.Repeat("x ", 1024) + "infix left")
		i := semanticLastIdent(t, a, "left")
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) { return a.inOperatorsBodyControlled(i, c), nil })
	})
	t.Run("previous-comment", func(t *testing.T) {
		a := analyze("foo " + strings.Repeat("/*c*/ ", 1024) + "left")
		i := semanticLastIdent(t, a, "left")
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) {
			occ := 0
			value, _ := a.classifyControlled(i, &occ, c)
			return value, nil
		})
	})
	t.Run("rule-header", func(t *testing.T) {
		a := analyze("rule expr " + strings.Repeat("T ", 1024) + "= operators { infix left")
		i := semanticLastIdent(t, a, "left")
		at := semanticBodyPrefixChecks(t, a, i, false) + 2
		assertOccurrencePhaseCancels(t, at, func(c *renderControl) (any, *rpcError) { return a.inOperatorsBodyControlled(i, c), nil })
	})
	t.Run("forward-body", func(t *testing.T) {
		a := analyze(semanticOperatorsSource(1024))
		i := semanticLastIdent(t, a, "left")
		at := semanticBodyPrefixChecks(t, a, i, true) + 2
		assertOccurrencePhaseCancels(t, at, func(c *renderControl) (any, *rpcError) { return a.inOperatorsBodyControlled(i, c), nil })
	})
}

func TestSemanticRequestsShareAnalysis(t *testing.T) {
	a := analyze(semanticOperatorsSource(128))
	var wg sync.WaitGroup
	for range 4 {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		wg.Go(func() {
			if _, err := semanticServer(a, ctx).request(semanticMessage()); err != nil {
				t.Errorf("shared analysis: %+v", err)
			}
		})
	}
	wg.Wait()
}

func TestSemanticRequestProtocolInvalidation(t *testing.T) {
	for _, change := range []bool{false, true} {
		name := "cancel"
		if change {
			name = "change"
		}
		t.Run(name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var enterOnce, releaseOnce sync.Once
			releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
			requestHook = func(ctx context.Context, method string) {
				if method == "textDocument/semanticTokens/full" {
					blocked := false
					enterOnce.Do(func() { close(started); blocked = true })
					if blocked {
						<-release
					}
				}
			}
			t.Cleanup(func() { requestHook = nil })
			c := newResponsiveClient(t, false)
			defer func() { releaseWorker(); c.exit() }()
			text := semanticDenseSource(2048)
			c.open(semanticURI, text)
			msg := semanticMessage()
			var params map[string]any
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				t.Fatal(err)
			}
			asyncRequest(c, 100, msg.Method, params)
			awaitBarrier(t, started)
			code := codeRequestCancelled
			if change {
				code = codeContentModified
				text = semanticDenseSource(32)
				c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": semanticURI, "version": 2}, "contentChanges": []any{map[string]any{"text": text}}})
			} else {
				c.notify("$/cancelRequest", map[string]any{"id": 100})
			}
			assertResponseError(t, c.response("100"), code)
			releaseWorker()
			var got struct {
				Data []int `json:"data"`
			}
			c.requestInto(msg.Method, params, &got)
			if !reflect.DeepEqual(got.Data, analyze(text).semanticTokens()) {
				t.Fatal("retry semantic data differs")
			}
		})
	}
}

func TestSemanticInteriorPreservesForeignPanic(t *testing.T) {
	a := analyze("/*" + strings.Repeat("line\n", 512) + "*/")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := &struct{ n int }{1}
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("foreign panic changed: %v", got)
		}
	}()
	renderRequest(&occurrencePanicContext{Context: ctx, marker: marker}, func(c *renderControl) (any, *rpcError) { return a.semanticTokensControlled(c), nil })
	t.Fatal("foreign panic was swallowed")
}
