package lint

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestLintFormattingCancelsWithinDiagnostics(t *testing.T) {
	term := &grammar.StringLit{Value: strings.Repeat("😀", 4096)}
	expr := &grammar.Literal{Value: strings.Repeat("😀", 4096)}
	for _, name := range []string{"show", "predicate-comparison", "attribute-comparison"} {
		t.Run(name, func(t *testing.T) {
			// Admission succeeds; the next check must come from within formatting.
			c := &lintControl{ctx: lintTestContext(t, 2)}
			defer func() {
				got, ok := recover().(lintCancellation)
				if !ok || !errors.Is(got.err, context.Canceled) {
					t.Fatalf("wrong unwind: %+v", got)
				}
			}()
			switch name {
			case "show":
				showControlled(expr, c)
			case "predicate-comparison":
				sameControlled(&grammar.Predicate{Term: term}, &grammar.Predicate{Term: term}, false, c)
			case "attribute-comparison":
				attr := &grammar.Attribute{Name: "recover", Pragma: true, KwArgs: map[string]any{"skip": expr}}
				e := &grammar.Attributed{Expr: expr, Attrs: []*grammar.Attribute{attr}}
				sameControlled(e, e, false, c)
			}
			t.Fatal("formatting completed without cancellation")
		})
	}
}

func TestLintShortMessageParity(t *testing.T) {
	for _, text := range []string{"", strings.Repeat("😀", 36), strings.Repeat("x", 37), strings.Repeat("😀", 38), strings.Repeat("x", 41), strings.Repeat("😀", 8192), strings.Repeat("\xff", 60)} {
		expr := &grammar.Ref{Name: text}
		want := grammar.FormatExpr(expr)
		if r := []rune(want); len(r) > 40 {
			want = string(r[:37]) + "..."
		}
		want = "`" + want + "`"
		for _, c := range []*lintControl{nil, {ctx: lintTestContext(t, 0)}} {
			if got := showControlled(expr, c); got != want {
				t.Fatalf("message=%q want=%q", got, want)
			}
		}
	}
}
