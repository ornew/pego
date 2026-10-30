package compile

import (
	"context"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/starttest"
)

func BenchmarkClassCompileCancellation(b *testing.B) {
	ranges := make([]grammar.CharRange, 512)
	for i := range ranges {
		ranges[i] = grammar.CharRange{Lo: rune(256 + 2*i), Hi: rune(256 + 2*i)}
	}
	class := &grammar.CharClass{Ranges: ranges}
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Choice{Alts: []grammar.Expr{class, &grammar.Discard{Expr: &grammar.Repeat{Expr: class, Min: 1, Max: -1}}}}}}}}
	starttest.MarkAll(g)
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
					normalizationBenchmarkProgram, err = compileContext(ctx, g, Options{})
				} else {
					normalizationBenchmarkProgram, err = compileStatic(g, Options{}, nil)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
