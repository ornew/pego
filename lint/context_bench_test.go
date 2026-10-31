package lint

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/syntax"
)

var proofLintBenchmarkFindings []Finding

func BenchmarkSuccessProofLintCancellation(b *testing.B) {
	sources := map[string]string{"attributes": `rule main = "a"` + strings.Repeat(` @error("label")`, 64)}
	for name, path := range map[string]string{"source-grammar": "../zeroparse.zp", "json": "../parsers/json/json.zp"} {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		sources[name] = string(data)
	}
	for _, name := range []string{"json", "source-grammar", "attributes"} {
		g, err := syntax.Parse(sources[name])
		if err != nil {
			b.Fatal(err)
		}
		for _, controlled := range []bool{false, true} {
			mode := "ordinary"
			if controlled {
				mode = "cancellable"
			}
			b.Run(name+"/"+mode, func(b *testing.B) {
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
}
