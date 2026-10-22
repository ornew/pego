package lsp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCodeActionResponsesControlledParity(t *testing.T) {
	for _, tc := range codeActionResponseCorpus(t) {
		t.Run(tc.name, func(t *testing.T) {
			tokens := append([]token(nil), tc.a.toks...)
			occs := append([]occurrence(nil), tc.a.occs...)
			before, err := json.Marshal(tc.a.g)
			if err != nil {
				t.Fatal(err)
			}
			value, rerr := codeActionServer(tc.a, nil, tc.enabled, tc.explain, tc.edit).request(tc.msg)
			want := occurrenceResponseJSON(t, value, rerr)
			for _, ctx := range []context.Context{context.Background(), formatContext(t, 0)} {
				got, gerr := codeActionServer(tc.a, ctx, tc.enabled, tc.explain, tc.edit).request(tc.msg)
				if string(occurrenceResponseJSON(t, got, gerr)) != string(want) {
					t.Fatalf("controlled response differs: %v/%+v want %s", got, gerr, want)
				}
			}
			after, err := json.Marshal(tc.a.g)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) || !reflect.DeepEqual(tokens, tc.a.toks) || !reflect.DeepEqual(occs, tc.a.occs) {
				t.Fatal("request changed shared analysis")
			}
			if strings.HasPrefix(tc.name, "modified/") {
				n := 0
				if tc.name == "modified/severity" {
					n = 2
				}
				if rerr != nil || len(value.([]codeAction)) != n {
					t.Fatalf("diagnostic identity changed: %v/%+v", value, rerr)
				}
			}
			if strings.HasSuffix(tc.name, "capabilities7") {
				actions := value.([]codeAction)
				if rerr != nil || len(actions) != 2 || actions[0].Command == nil || actions[1].Edit == nil {
					t.Fatalf("action order: %v/%+v", value, rerr)
				}
				change := actions[1].Edit.DocumentChanges[0]
				if change.TextDocument.URI != codeActionURI || change.TextDocument.Version != 7 {
					t.Fatalf("unguarded edit: %+v", change)
				}
			}
		})
	}
}

func TestCodeActionRequestsCancelAndRetry(t *testing.T) {
	for _, tc := range codeActionBenchmarkCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			value, err := codeActionServer(tc.a, nil, tc.enabled, tc.explain, tc.edit).request(tc.msg)
			want := occurrenceResponseJSON(t, value, err)
			count := formatContext(t, 0)
			if _, err := codeActionServer(tc.a, count, tc.enabled, tc.explain, tc.edit).request(tc.msg); err != nil {
				t.Fatal(err)
			}
			if count.checks < 2 {
				t.Fatalf("missing entry/final checks: %d", count.checks)
			}
			for _, at := range []int{1, 2, max(2, count.checks/2), count.checks} {
				got, gerr := codeActionServer(tc.a, formatContext(t, at), tc.enabled, tc.explain, tc.edit).request(tc.msg)
				if got != nil || gerr == nil || gerr.Code != codeRequestCancelled {
					t.Fatalf("check%d: partial response %T/%+v", at, got, gerr)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, gerr := codeActionServer(tc.a, ctx, tc.enabled, tc.explain, tc.edit).request(tc.msg)
			if string(occurrenceResponseJSON(t, got, gerr)) != string(want) {
				t.Fatal("retry response changed")
			}
		})
	}
}

func TestCodeActionInteriorWalksCancel(t *testing.T) {
	a := codeActionAnalysis(t, codeActionSource("\n"))
	d := codeActionTarget(t, a)
	t.Run("only", func(t *testing.T) {
		only := make([]string, 4096)
		for i := range only {
			only[i] = "refactor"
		}
		msg := codeActionMessage(d.Range, []Diagnostic{d}, only)
		assertOccurrencePhaseCancels(t, 2, func(c *renderControl) (any, *rpcError) {
			return codeActionServer(a, c.ctx, true, true, true).codeActionsControlled(msg.Params, c)
		})
	})
	t.Run("supplied", func(t *testing.T) {
		// Count checks before matching, without renderRequest's preflight. The
		// next check in the real request must occur in the supplied-diagnostic walk.
		count := formatContext(t, 0)
		c := &renderControl{ctx: count}
		c.poll()
		if _, err := codeActionServer(a, count, true, true, false).docs[codeActionURI].lintDiagnosticsContext(count); err != nil {
			t.Fatal(err)
		}
		c.poll()
		if err := count.Err(); err != nil {
			t.Fatal(err)
		}
		msg := codeActionMessage(d.Range, codeActionSupplied(d, 4096, false), nil)
		assertOccurrencePhaseCancels(t, count.checks+2, func(c *renderControl) (any, *rpcError) {
			return codeActionServer(a, c.ctx, true, true, false).codeActionsControlled(msg.Params, c)
		})
	})
	t.Run("suppression-tokens", func(t *testing.T) {
		// This line refuses an edit before candidate analysis, so compiler polls
		// cannot hide a missing check in the preceding token walk.
		long := analyze("@start\nrule main = " + strings.Repeat("\"a\" ", 4096) + "\n// lint:ignore\n")
		doc := codeActionServer(long, nil, true, true, true).docs[codeActionURI]
		target := Diagnostic{Code: "unused-capture", Range: Range{Start: Position{Line: 2}}}
		assertOccurrencePhaseCancels(t, 3, func(c *renderControl) (any, *rpcError) {
			edit, ok := doc.suppressionEditControlled(c.ctx, target, c)
			return struct {
				Edit TextEdit
				OK   bool
			}{edit, ok}, nil
		})
	})
}

func TestCodeActionCandidateCancellationUnwinds(t *testing.T) {
	a := codeActionAnalysis(t, codeActionSource("\n"))
	target := codeActionTarget(t, a)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := false
	analyzeHook = func(text string) {
		if strings.Contains(text, "// lint:ignore") {
			entered = true
			cancel()
		}
	}
	t.Cleanup(func() { analyzeHook = nil })
	s := codeActionServer(a, ctx, true, true, true)
	msg := codeActionMessage(target.Range, []Diagnostic{target}, nil)
	completed := false
	got, err := renderRequest(ctx, func(c *renderControl) (any, *rpcError) {
		value, rerr := s.codeActionsControlled(msg.Params, c)
		completed = true
		return value, rerr
	})
	if !entered || completed || got != nil || err == nil || err.Code != codeRequestCancelled {
		t.Fatalf("entered=%v phase completed=%v result=%T error=%+v", entered, completed, got, err)
	}
	analyzeHook = nil
	value, rerr := codeActionServer(a, nil, true, true, true).request(msg)
	if rerr != nil || len(value.([]codeAction)) != 2 {
		t.Fatalf("retry failed: %v/%+v", value, rerr)
	}
}

func TestCodeActionRequestsShareAnalysis(t *testing.T) {
	a := codeActionAnalysis(t, codeActionSource("\n"))
	target := codeActionTarget(t, a)
	msg := codeActionMessage(target.Range, []Diagnostic{target}, nil)
	var wg sync.WaitGroup
	for range 4 {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		wg.Go(func() {
			got, err := codeActionServer(a, ctx, true, true, true).request(msg)
			if err != nil || len(got.([]codeAction)) != 2 {
				t.Errorf("shared analysis: %v/%+v", got, err)
			}
		})
	}
	wg.Wait()
}

func TestCodeActionInteriorPreservesForeignPanic(t *testing.T) {
	a := codeActionAnalysis(t, codeActionSource("\n"))
	target := codeActionTarget(t, a)
	only := make([]string, 4096)
	for i := range only {
		only[i] = "refactor"
	}
	msg := codeActionMessage(target.Range, []Diagnostic{target}, only)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := &struct{ n int }{1}
	pc := &occurrencePanicContext{Context: ctx, marker: marker}
	defer func() {
		if got := recover(); got != marker {
			t.Fatalf("foreign panic changed: %v", got)
		}
	}()
	renderRequest(pc, func(c *renderControl) (any, *rpcError) {
		return codeActionServer(a, pc, true, true, true).codeActionsControlled(msg.Params, c)
	})
	t.Fatal("foreign panic was swallowed")
}

func TestCodeActionRequestProtocolInvalidation(t *testing.T) {
	for _, candidate := range []bool{false, true} {
		for _, change := range []bool{false, true} {
			name := "entry"
			if candidate {
				name = "candidate"
			}
			if change {
				name += "/change"
			} else {
				name += "/cancel"
			}
			t.Run(name, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var enterOnce, releaseOnce sync.Once
				releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
				block := func() {
					blocked := false
					enterOnce.Do(func() { close(started); blocked = true })
					if blocked {
						<-release
					}
				}
				if candidate {
					analyzeHook = func(text string) {
						if strings.Contains(text, "// lint:ignore") {
							block()
						}
					}
				} else {
					requestHook = func(ctx context.Context, method string) {
						if method == "textDocument/codeAction" {
							block()
						}
					}
				}
				t.Cleanup(func() { requestHook = nil; analyzeHook = nil })
				c := newResponsiveClient(t, true)
				defer func() { releaseWorker(); c.exit() }()
				diagnostics := c.open(codeActionURI, codeActionSource("\n"))
				target := func(ds []Diagnostic) Diagnostic {
					for _, d := range ds {
						if d.Code == "unused-capture" {
							return d
						}
					}
					t.Fatal("missing target")
					return Diagnostic{}
				}
				d := target(diagnostics)
				msg := codeActionMessage(d.Range, []Diagnostic{d}, nil)
				var params map[string]any
				if err := json.Unmarshal(msg.Params, &params); err != nil {
					t.Fatal(err)
				}
				asyncRequest(c, 100, msg.Method, params)
				awaitBarrier(t, started)
				code := codeRequestCancelled
				if change {
					code = codeContentModified
					c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": codeActionURI, "version": 2}, "contentChanges": []any{map[string]any{"text": strings.Replace(codeActionSource("\n"), `b:"b"`, `b:"c"`, 1)}}})
				} else {
					c.notify("$/cancelRequest", map[string]any{"id": 100})
				}
				assertResponseError(t, c.response("100"), code)
				releaseWorker()
				if change {
					current := c.diagnostics(codeActionURI)
					if current.Version == nil || *current.Version != 2 {
						t.Fatalf("wrong retry version: %+v", current.Version)
					}
					d = target(current.Diagnostics)
					if err := json.Unmarshal(codeActionMessage(d.Range, []Diagnostic{d}, nil).Params, &params); err != nil {
						t.Fatal(err)
					}
				}
				var got []codeAction
				c.requestInto(msg.Method, params, &got)
				if len(got) != 2 || got[0].Command == nil || got[1].Edit == nil {
					t.Fatalf("retry actions: %+v", got)
				}
			})
		}
	}
}
