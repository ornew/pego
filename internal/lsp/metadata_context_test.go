package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/syntax"
)

type metadataPredicateContext struct {
	context.Context
	condition func() bool
	failure   error
}

func (c *metadataPredicateContext) Err() error {
	if c.failure != nil {
		return c.failure
	}
	if c.condition() {
		c.failure = context.Canceled
	}
	return c.failure
}

func TestMetadataParityAndImmutability(t *testing.T) {
	for _, s := range append(semanticResponseSources(t), metadataSources(t)[2:]...) {
		t.Run(s.name, func(t *testing.T) {
			want := metadataFingerprint(t, analyze(s.text))
			for _, ctx := range []context.Context{context.Background(), formatContext(t, 0), &metadataPredicateContext{Context: context.Background(), condition: func() bool { return false }}} {
				a, err := analyzeContext(ctx, s.text)
				if err != nil {
					t.Fatal(err)
				}
				if got := metadataFingerprint(t, a); string(got) != string(want) {
					t.Fatal("analysis differs with context")
				}
			}
			seed := metadataPrepared(t, s.text)
			before, _ := json.Marshal(seed.g)
			original := seed.g.Clone()
			a := metadataClone(seed)
			if err := a.finishMetadataContext(formatContext(t, 0)); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(seed.g)
			if string(before) != string(after) || !reflect.DeepEqual(original, seed.g) {
				t.Fatal("metadata mutated syntax tree")
			}
		})
	}
	a := analyze("rule a = \"a\"\nrule a = \"b\"\n")
	if len(a.defs) != 2 || a.rules["a"] != a.defs[0] {
		t.Fatal("duplicate definition changed first-definition lookup")
	}
}
func TestMetadataAnalysisCancelsAndRetries(t *testing.T) {
	source := metadataWideSource(512, false)
	count := formatContext(t, 0)
	a, err := analyzeContext(count, source)
	if err != nil {
		t.Fatal(err)
	}
	want := metadataFingerprint(t, a)
	for _, at := range []int{1, 2, count.checks / 2, count.checks} {
		got, err := analyzeContextSafely(formatContext(t, at), source)
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("check%d: %v/%v", at, got, err)
		}
	}
	got, err := analyzeContext(context.Background(), source)
	if err != nil || string(metadataFingerprint(t, got)) != string(want) {
		t.Fatal("retry differs")
	}
}
func assertMetadataPhaseCancels(t *testing.T, phase func(*renderControl)) {
	t.Helper()
	completed := false
	err := runMetadataContext(formatContext(t, 2), func(c *renderControl) error { phase(c); completed = true; return nil })
	if completed || !errors.Is(err, context.Canceled) {
		t.Fatalf("completed=%v error=%v", completed, err)
	}
}
func TestMetadataInteriorCancellation(t *testing.T) {
	const n = 4096
	t.Run("declarations", func(t *testing.T) {
		a := metadataPrepared(t, semanticDenseSource(n))
		assertMetadataPhaseCancels(t, a.indexDeclarationStartsControlled)
	})
	t.Run("definition", func(t *testing.T) {
		a := metadataPrepared(t, "@start\nrule main = "+strings.Repeat("\"a\" ", n))
		a.indexDeclarationStarts()
		r := a.g.File.Decls[0].(*grammar.RuleDef)
		assertMetadataPhaseCancels(t, func(c *renderControl) { a.define(kindRule, r.Name, r.Pos, r.Diagnostic(), c) })
	})
	t.Run("documentation", func(t *testing.T) {
		a := metadataPrepared(t, strings.Repeat("// documentation\n", n)+"@start\nrule main = \"a\"")
		a.indexDeclarationStarts()
		r := a.g.File.Decls[0].(*grammar.RuleDef)
		assertMetadataPhaseCancels(t, func(c *renderControl) { a.define(kindRule, r.Name, r.Pos, r.Diagnostic(), c) })
	})
	t.Run("comment-trim", func(t *testing.T) {
		assertMetadataPhaseCancels(t, func(c *renderControl) { commentText("/*"+strings.Repeat(" \t\u2000", n)+"*/", c) })
	})
	t.Run("type", func(t *testing.T) {
		u := &grammar.JoinType{}
		for range n {
			u.Types = append(u.Types, &grammar.TypeRef{Name: "T"})
		}
		a := newAnalysis("")
		assertMetadataPhaseCancels(t, func(c *renderControl) { a.walkType(u, c) })
	})
	t.Run("expression", func(t *testing.T) {
		s := &grammar.Seq{}
		for range n {
			s.Items = append(s.Items, &grammar.Literal{Value: "a"})
		}
		a := newAnalysis("")
		assertMetadataPhaseCancels(t, func(c *renderControl) { a.walkExpr(s, c) })
	})
	t.Run("action", func(t *testing.T) {
		call := &grammar.Call{Func: "list"}
		for range n {
			call.Args = append(call.Args, &grammar.IntLit{Value: 1})
		}
		a := newAnalysis("")
		assertMetadataPhaseCancels(t, func(c *renderControl) { a.walkTerm(call, c) })
	})
	for _, keyword := range []bool{false, true} {
		name := "attribute-args"
		if keyword {
			name = "attribute-keywords"
		}
		t.Run(name, func(t *testing.T) {
			at := &grammar.Attribute{Name: "custom", KwArgs: map[string]any{}}
			for i := range n {
				if keyword {
					at.KwArgs[fmt.Sprint(i)] = i
				} else {
					at.Args = append(at.Args, i)
				}
			}
			a := newAnalysis("")
			assertMetadataPhaseCancels(t, func(c *renderControl) {
				a.walkExpr(&grammar.Attributed{Expr: &grammar.Literal{Value: "a"}, Attrs: []*grammar.Attribute{at}}, c)
			})
		})
	}
	t.Run("syntax-diagnostics", func(t *testing.T) {
		a := newAnalysis("x")
		for range n {
			a.syntaxErrs = append(a.syntaxErrs, &syntax.Error{Msg: "invalid"})
		}
		assertMetadataPhaseCancels(t, a.mapSyntaxDiagnostics)
	})
	t.Run("compile-diagnostics", func(t *testing.T) {
		a := newAnalysis("x")
		for i := range n {
			a.defs = append(a.defs, &definition{kwStart: i + 1})
		}
		list := engine.ErrorList{&engine.Error{Msg: "undefined"}}
		assertMetadataPhaseCancels(t, func(c *renderControl) { a.mapCompileDiagnostics(list, c) })
	})
	t.Run("definition-range", func(t *testing.T) {
		a := newAnalysis("x")
		for i := range n {
			a.defs = append(a.defs, &definition{kwStart: i + 1})
		}
		assertMetadataPhaseCancels(t, func(c *renderControl) { a.errorRangeControlled(grammar.Pos{}, true, c) })
	})
	t.Run("occurrence-sort", func(t *testing.T) {
		a := newAnalysis("")
		for i := range n {
			a.occs = append(a.occs, occurrence{start: n - i})
		}
		assertMetadataPhaseCancels(t, a.sortOccurrences)
	})
	t.Run("diagnostic-sort", func(t *testing.T) {
		a := newAnalysis("")
		for i := range n {
			a.diags = append(a.diags, Diagnostic{Range: Range{Start: Position{Line: n - i}}})
		}
		assertMetadataPhaseCancels(t, a.sortDiagnostics)
	})
}
func TestMetadataStableOrdering(t *testing.T) {
	a := newAnalysis("")
	a.occs = []occurrence{{name: "second", start: 2}, {name: "first", start: 1}, {name: "tie", start: 1}}
	a.diags = []Diagnostic{{Message: "second", Range: Range{Start: Position{Line: 2}}}, {Message: "first", Range: Range{Start: Position{Line: 1}}}, {Message: "tie", Range: Range{Start: Position{Line: 1}}}}
	if err := runMetadataContext(formatContext(t, 0), func(c *renderControl) error {
		a.sortOccurrences(c)
		a.sortDiagnostics(c)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if a.occs[0].name != "first" || a.occs[1].name != "tie" || a.diags[0].Message != "first" || a.diags[1].Message != "tie" {
		t.Fatal("stable order changed")
	}
}
func TestMetadataCompileRollback(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			source := metadataWideSource(512, !success)
			a := metadataPrepared(t, source)
			a.indexDeclarationStarts()
			a.collect()
			a.diags = []Diagnostic{{Message: "previous"}}
			old := append([]Diagnostic(nil), a.diags...)
			before, _ := json.Marshal(a.g)
			original := a.g.Clone()
			ctx := &metadataPredicateContext{Context: context.Background(), condition: func() bool {
				if success {
					return a.prog != nil
				}
				return len(a.diags) > len(old)
			}}
			if err := a.compileContext(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
			if a.prog != nil || !reflect.DeepEqual(a.diags, old) {
				t.Fatal("cancelled compilation published a partial result")
			}
			after, _ := json.Marshal(a.g)
			if string(before) != string(after) || !reflect.DeepEqual(original, a.g) {
				t.Fatal("compiler metadata changed syntax")
			}
			if err := a.compileContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if success && a.prog == nil || !success && len(a.diags) <= len(old) {
				t.Fatal("retry did not complete")
			}
		})
	}
}
func TestMetadataOriginalErrorAndForeignPanic(t *testing.T) {
	ctx := &metadataPredicateContext{Context: context.Background(), condition: func() bool { return false }, failure: context.DeadlineExceeded}
	if err := runMetadataContext(ctx, func(*renderControl) error { t.Fatal("entered cancelled operation"); return nil }); err != context.DeadlineExceeded {
		t.Fatal(err)
	}
	marker := &struct{ n int }{1}
	defer func() {
		if recover() != marker {
			t.Fatal("foreign panic changed")
		}
	}()
	runMetadataContext(formatContext(t, 0), func(*renderControl) error { panic(marker) })
}
func TestMetadataSharedSyntax(t *testing.T) {
	seed := metadataPrepared(t, metadataWideSource(128, false))
	before, _ := json.Marshal(seed.g)
	original := seed.g.Clone()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			a := metadataClone(seed)
			if err := metadataBenchmarkCollect(formatContext(t, 0), a); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	after, _ := json.Marshal(seed.g)
	if string(before) != string(after) || !reflect.DeepEqual(original, seed.g) {
		t.Fatal("shared syntax changed")
	}
}

type metadataQueueContext struct {
	context.Context
	ready   func() bool
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (c *metadataQueueContext) Err() error {
	if c.ready() {
		c.once.Do(func() { close(c.started); <-c.release })
	}
	return c.Context.Err()
}
func TestMetadataQueueSupersededAnalysis(t *testing.T) {
	seed := metadataPrepared(t, metadataWideSource(512, false))
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	outcome := make(chan error, 1)
	q := newAnalysisQueue(func(ctx context.Context, s analysisSnapshot) (*analysis, error) {
		a := metadataClone(seed)
		if s.Version == 1 {
			ctx = &metadataQueueContext{Context: ctx, ready: func() bool { return len(a.defs) >= 16 }, started: started, release: release}
		}
		if err := a.finishMetadataContext(ctx); err != nil {
			if s.Version == 1 {
				outcome <- err
			}
			return nil, err
		}
		return a, nil
	})
	defer func() { unblock(); q.stop() }()
	q.submit(analysisSnapshot{URI: "file:///metadata", Version: 1})
	awaitBarrier(t, started)
	q.submit(analysisSnapshot{URI: "file:///metadata", Version: 2})
	unblock()
	select {
	case err := <-outcome:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("old analysis failed to stop")
	}
	select {
	case result := <-q.results:
		if result.Snapshot.Version != 2 || result.Analysis == nil || result.Err != nil {
			t.Fatalf("partial/stale publication: %+v", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("replacement analysis did not finish")
	}
}
