package lint

import (
	"context"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

func BenchmarkClassLintCancellation(b *testing.B) {
	ranges := make([]grammar.CharRange, 512)
	for i := range ranges {
		ranges[i] = grammar.CharRange{Lo: rune(256 + 2*(len(ranges)-i)), Hi: rune(256 + 2*(len(ranges)-i))}
	}
	class := &grammar.CharClass{Ranges: ranges}
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Choice{Alts: []grammar.Expr{class, &grammar.Literal{Value: "a"}, &grammar.CharClass{Ranges: ranges}}}}}}}
	for _, controlled := range []bool{false, true} {
		name := "ordinary"
		if controlled {
			name = "cancellable"
		}
		b.Run(name, func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if controlled {
					proofLintBenchmarkFindings, err = runContext(ctx, g, "main", options{})
				} else {
					proofLintBenchmarkFindings = runUnchecked(g, "main", options{})
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
