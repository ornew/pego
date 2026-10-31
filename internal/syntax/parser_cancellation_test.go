package syntax

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

func TestParsePartialContextCancelsWithinLongStatement(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{"ASCII", "rule main = " + strings.Repeat(`"x" `, 2048)},
		{"Unicode", "rule main = " + strings.Repeat(`"😀" `, 2048)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Count lexical polls first. The next poll is the parser preflight;
			// cancellation on the following poll must occur within the rule.
			baseline := &cancelOnCheck{cancelAt: int(^uint(0) >> 1)}
			if _, err := lexContext(baseline, tc.source, &errorList{}); err != nil {
				t.Fatal(err)
			}
			ctx := &cancelOnCheck{cancelAt: baseline.checks + 2}
			g, diagnostics, err := ParsePartialContext(ctx, tc.source)
			if g != nil || diagnostics != nil || !errors.Is(err, context.Canceled) || ctx.checks != ctx.cancelAt {
				t.Fatalf("cancelled parse = %v, %v, %v after %d/%d polls", g, diagnostics, err, ctx.checks, ctx.cancelAt)
			}
			wantG, wantDiagnostics := ParsePartial(tc.source)
			gotG, gotDiagnostics, err := ParsePartialContext(context.Background(), tc.source)
			if err != nil || !reflect.DeepEqual(gotG, wantG) || !reflect.DeepEqual(gotDiagnostics, wantDiagnostics) {
				t.Fatalf("parse parity: err=%v, grammar equal=%v, diagnostics equal=%v", err, reflect.DeepEqual(gotG, wantG), reflect.DeepEqual(gotDiagnostics, wantDiagnostics))
			}
		})
	}
}

func TestParseCancellationDuringLookaheadAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*parser)
	}{
		{"lambda lookahead", func(p *parser) { p.tryLambda() }},
		{"recovery scan", func(p *parser) {
			p.i = len(p.toks) - 1
			defer p.recover()
			panic(bail{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &cancelOnCheck{cancelAt: 1}
			toks := make([]token, 1025)
			for i := range toks {
				toks[i] = token{kind: tIdent, text: "x"}
			}
			toks[0] = token{kind: tPunct, text: "("}
			toks[len(toks)-1] = token{kind: tEOF}
			if tc.name == "lambda lookahead" {
				for i := 2; i < len(toks)-1; i += 2 {
					toks[i] = token{kind: tPunct, text: ","}
				}
			}
			p := &parser{toks: toks, errs: &errorList{}, ctx: ctx}
			defer func() {
				if value := recover(); value != nil {
					cancelled, ok := value.(parseCancelled)
					if !ok || !errors.Is(cancelled.err, context.Canceled) || ctx.checks != 1 {
						t.Fatalf("wanted cancellation during %s, got %v after %d polls", tc.name, value, ctx.checks)
					}
					return
				}
				t.Fatalf("%s did not cancel", tc.name)
			}()
			tc.run(p)
		})
	}
}

func TestParsePartialContextChecksAfterShortParse(t *testing.T) {
	const source = `rule main = "x"`
	baseline := &cancelOnCheck{cancelAt: int(^uint(0) >> 1)}
	if _, err := lexContext(baseline, source, &errorList{}); err != nil {
		t.Fatal(err)
	}
	// The short parse consumes fewer than 256 tokens; the final check must
	// discard the completed result when cancellation arrives after preflight.
	ctx := &cancelOnCheck{cancelAt: baseline.checks + 2}
	g, diagnostics, err := ParsePartialContext(ctx, source)
	if g != nil || diagnostics != nil || !errors.Is(err, context.Canceled) || ctx.checks != ctx.cancelAt {
		t.Fatalf("short cancelled parse = %v, %v, %v after %d/%d polls", g, diagnostics, err, ctx.checks, ctx.cancelAt)
	}
}

func TestParseCancellationDuringCommentTransfer(t *testing.T) {
	comments := make([]*grammar.Comment, 1024)
	for i := range comments {
		comments[i] = &grammar.Comment{}
	}
	for _, tc := range []struct {
		name string
		run  func(*parser)
	}{
		{"takeComments", func(p *parser) {
			p.cur = &grammar.LineBreak{}
			p.takeComments(0)
		}},
		{"lineBreak", func(p *parser) { p.lineBreak() }},
		{"takeTermLayout", func(p *parser) { p.takeTermLayout() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &cancelOnCheck{cancelAt: 2}
			p := &parser{
				toks: []token{{comments: comments, lineComment: &grammar.Comment{}}},
				errs: &errorList{}, ctx: ctx,
			}
			defer func() {
				value := recover()
				cancelled, ok := value.(parseCancelled)
				if !ok || !errors.Is(cancelled.err, context.Canceled) || ctx.checks != 2 {
					t.Fatalf("wanted cancellation during %s copy, got %v after %d polls", tc.name, value, ctx.checks)
				}
			}()
			tc.run(p)
		})
	}
}

func TestParseCancellationDuringAttributeBreakPadding(t *testing.T) {
	// A break after many same-line attributes requires a large nil prefix.
	ctx := &cancelOnCheck{cancelAt: 2}
	p := &parser{ctx: ctx}
	a := &grammar.Attributed{Attrs: make([]*grammar.Attribute, 2048)}
	defer func() {
		value := recover()
		cancelled, ok := value.(parseCancelled)
		if !ok || !errors.Is(cancelled.err, context.Canceled) || ctx.checks != 2 || len(a.Breaks) != 256 {
			t.Fatalf("wanted cancellation during attribute padding, got %v after %d polls and %d breaks", value, ctx.checks, len(a.Breaks))
		}
	}()
	p.appendAttributeBreak(a, &grammar.LineBreak{})
}

func TestParsePartialContextLongSameLineAttributesParity(t *testing.T) {
	source := `rule main = ("x" ` + strings.Repeat(`@a `, 1024) + "\n@a)"
	wantG, wantDiagnostics := ParsePartial(source)
	gotG, gotDiagnostics, err := ParsePartialContext(context.Background(), source)
	if err != nil || !reflect.DeepEqual(gotG, wantG) || !reflect.DeepEqual(gotDiagnostics, wantDiagnostics) {
		t.Fatalf("attribute break parity: err=%v, grammar equal=%v, diagnostics equal=%v", err, reflect.DeepEqual(gotG, wantG), reflect.DeepEqual(gotDiagnostics, wantDiagnostics))
	}
}
