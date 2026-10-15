package lint

import (
	"context"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func BenchmarkDiagnosticFormatCancellation(b *testing.B) {
	sources := map[string]string{
		"predicates":      "rule main = " + strings.TrimSuffix(strings.Repeat("{true} / ", 32), " / "),
		"long-diagnostic": `rule main = "" / "` + strings.Repeat("x", 8192) + `"`,
	}
	for _, name := range []string{"predicates", "long-diagnostic"} {
		g, err := syntax.Parse(sources[name])
		if err != nil {
			b.Fatal(err)
		}
		for _, active := range []bool{false, true} {
			mode := "ordinary"
			if active {
				mode = "cancellable"
			}
			b.Run(name+"/"+mode, func(b *testing.B) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				b.ReportAllocs()
				for b.Loop() {
					if active {
						proofLintBenchmarkFindings, err = RunContext(ctx, g, "main", Options{})
					} else {
						proofLintBenchmarkFindings = Run(g, "main", Options{})
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
