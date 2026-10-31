package lsp

import (
	"context"
	"os"
	"strings"
	"testing"
)

var formatBenchmarkEdits []TextEdit

func BenchmarkFormatEditsCancellation(b *testing.B) {
	sources := map[string]string{"long-literal-crlf": "rule main = \"" + strings.Repeat("x", 8192) + "\"\r\n"}
	for name, path := range map[string]string{"json": "../../parsers/json/json.zp", "source-grammar": "../../zp.zp"} {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		sources[name] = string(data)
	}
	for _, name := range []string{"json", "source-grammar", "long-literal-crlf"} {
		a := analyze(sources[name])
		if len(a.syntaxErrs) != 0 {
			b.Fatal(a.syntaxErrs)
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
					var err error
					if active {
						formatBenchmarkEdits, err = formatEditsContext(ctx, a)
					} else {
						formatBenchmarkEdits = formatEdits(a)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
