package lsp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestOccurrenceResponsesControlledParity(t *testing.T) {
	for _, tc := range occurrenceResponseCorpus() {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]occurrence(nil), tc.a.occs...)
			value, err := occurrenceServer(tc.a, nil).request(tc.msg)
			want := occurrenceResponseJSON(t, value, err)
			if strings.HasSuffix(tc.name, "/declaration") && (value == nil || err != nil) {
				t.Fatalf("declaration fixture did not return a result: %v/%+v", value, err)
			}
			if strings.HasSuffix(tc.name, "/no-target") && (value != nil || err != nil) {
				t.Fatalf("empty position returned %v/%+v", value, err)
			}
			for _, ctx := range []context.Context{context.Background(), formatContext(t, 0)} {
				got, gerr := occurrenceServer(tc.a, ctx).request(tc.msg)
				if string(occurrenceResponseJSON(t, got, gerr)) != string(want) {
					t.Fatalf("controlled response differs: %v/%+v want %s", got, gerr, want)
				}
			}
			if !reflect.DeepEqual(before, tc.a.occs) {
				t.Fatal("request changed the shared occurrences")
			}
		})
	}
}

func TestOccurrenceRequestsCancelAndRetry(t *testing.T) {
	a := analyze(occurrenceSource(4096))
	if a.prog == nil {
		t.Fatal("fixture did not compile")
	}
	before := append([]occurrence(nil), a.occs...)
	for _, method := range []string{"references", "documentHighlight", "prepareRename", "rename"} {
		t.Run(method, func(t *testing.T) {
			msg := occurrenceMessage(method, Position{Line: 1, Character: 5}, true, "renamed")
			value, err := occurrenceServer(a, nil).request(msg)
			want := occurrenceResponseJSON(t, value, err)
			count := formatContext(t, 0)
			if _, err := occurrenceServer(a, count).request(msg); err != nil {
				t.Fatal(err)
			}
			if method != "prepareRename" && count.checks < 8 {
				t.Fatalf("fixture has too few checks: %d", count.checks)
			}
			for _, at := range []int{1, 2, max(2, count.checks/2), count.checks} {
				got, gerr := occurrenceServer(a, formatContext(t, at)).request(msg)
				if got != nil || gerr == nil || gerr.Code != codeRequestCancelled {
					t.Fatalf("check%d: partial response %T/%+v", at, got, gerr)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, gerr := occurrenceServer(a, ctx).request(msg)
			if string(occurrenceResponseJSON(t, got, gerr)) != string(want) {
				t.Fatal("retry response changed")
			}
		})
	}
	if !reflect.DeepEqual(before, a.occs) {
		t.Fatal("cancel/retry changed the shared occurrences")
	}
}

func assertOccurrencePhaseCancels(t *testing.T, at int, phase func(*renderControl) (any, *rpcError)) {
	t.Helper()
	completed := false
	got, err := renderRequest(formatContext(t, at), func(c *renderControl) (any, *rpcError) {
		value, rerr := phase(c)
		completed = true
		return value, rerr
	})
	if completed || got != nil || err == nil || err.Code != codeRequestCancelled {
		t.Fatalf("phase completed=%v result=%T error=%+v", completed, got, err)
	}
}

func TestOccurrenceInteriorWalksCancel(t *testing.T) {
	a := analyze(occurrenceSource(4096))
	o, ok := a.occurrenceAt(a.idx.offset(Position{Line: 1, Character: 5}))
	if !ok {
		t.Fatal("missing occurrence")
	}
	t.Run("collect", func(t *testing.T) {
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) { return a.occurrencesOfControlled(o, c), nil })
	})
	t.Run("unused-name", func(t *testing.T) {
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) { return nil, a.checkUnusedControlled(kindRule, "renamed", c) })
	})
	t.Run("new-name", func(t *testing.T) {
		empty := &analysis{}
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) {
			return nil, empty.checkNewNameControlled(kindRule, "old", strings.Repeat("a", 4096), c)
		})
	})
	for _, method := range []string{"references", "documentHighlight", "rename"} {
		t.Run("output/"+method, func(t *testing.T) {
			// Count only the preparation and collection polls, without a request
			// preflight. The next context check in the real call must therefore
			// occur inside output construction, before its final return check.
			count := formatContext(t, 0)
			c := &renderControl{ctx: count}
			c.poll() // controlled handler entry
			if method == "rename" {
				if err := a.checkNewNameControlled(kindRule, o.name, "renamed", c); err != nil {
					t.Fatal(err)
				}
			}
			a.occurrencesOfControlled(o, c)
			msg := occurrenceMessage(method, Position{Line: 1, Character: 5}, true, "renamed")
			s := occurrenceServer(a, nil)
			assertOccurrencePhaseCancels(t, count.checks+2, func(c *renderControl) (any, *rpcError) {
				switch method {
				case "references":
					return s.referencesControlled(msg.Params, c)
				case "documentHighlight":
					return s.documentHighlightControlled(msg.Params, c)
				default:
					return s.renameControlled(msg.Params, c)
				}
			})
		})
	}
}

func TestOccurrenceRequestsShareAnalysis(t *testing.T) {
	a := analyze(occurrenceSource(512))
	var wg sync.WaitGroup
	for _, method := range []string{"references", "documentHighlight", "prepareRename", "rename"} {
		msg := occurrenceMessage(method, Position{Line: 1, Character: 5}, true, "renamed")
		for range 4 {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			wg.Go(func() {
				if _, err := occurrenceServer(a, ctx).request(msg); err != nil {
					t.Errorf("shared analysis: %+v", err)
				}
			})
		}
	}
	wg.Wait()
}

func TestOccurrenceRequestProtocolInvalidation(t *testing.T) {
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
				if method == "textDocument/rename" {
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
			c.open(occurrenceURI, occurrenceSource(2048))
			msg := occurrenceMessage("rename", Position{Line: 1, Character: 5}, true, "renamed")
			var params map[string]any
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				t.Fatal(err)
			}
			asyncRequest(c, 100, msg.Method, params)
			awaitBarrier(t, started)
			code := codeRequestCancelled
			if change {
				code = codeContentModified
				c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": occurrenceURI, "version": 2}, "contentChanges": []any{map[string]any{"text": occurrenceSource(32)}}})
			} else {
				c.notify("$/cancelRequest", map[string]any{"id": 100})
			}
			assertResponseError(t, c.response("100"), code)
			releaseWorker()
			// A successful retry and orderly shutdown catch duplicate old replies.
			var edits workspaceEdit
			c.requestInto(msg.Method, params, &edits)
			want := 2049
			if change {
				want = 33
			}
			if len(edits.Changes[occurrenceURI]) != want {
				t.Fatalf("retry edits=%d want%d", len(edits.Changes[occurrenceURI]), want)
			}
		})
	}
}

type occurrencePanicContext struct {
	context.Context
	checks int
	marker any
}

func (c *occurrencePanicContext) Err() error {
	c.checks++
	if c.checks == 2 {
		panic(c.marker)
	}
	return nil
}

func TestOccurrenceInteriorPreservesForeignPanic(t *testing.T) {
	a := analyze(occurrenceSource(512))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := &struct{ n int }{1}
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("foreign panic changed: %v", got)
		}
	}()
	renderRequest(&occurrencePanicContext{Context: ctx, marker: marker}, func(c *renderControl) (any, *rpcError) {
		return a.occurrencesOfControlled(a.occs[0], c), nil
	})
	t.Fatal("foreign panic was swallowed")
}
