package grammar_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

var formatBenchmarkOutput string

func BenchmarkGrammarFormatCancellation(b *testing.B) {
	sources := map[string]string{"long-literal": `rule main = "` + strings.Repeat("x", 8192) + `"`}
	for name, path := range map[string]string{"json": "../parsers/json/json.pego", "source-grammar": "../pego.pego"} {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		sources[name] = string(data)
	}
	for _, name := range []string{"json", "source-grammar", "long-literal"} {
		g, err := syntax.Parse(sources[name])
		if err != nil {
			b.Fatal(err)
		}
		want := grammar.Format(g)
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
						formatBenchmarkOutput, err = grammar.FormatContext(ctx, g)
					} else {
						formatBenchmarkOutput = grammar.Format(g)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				if formatBenchmarkOutput != want {
					b.Fatal("formatting output changed")
				}
			})
		}
	}
}
