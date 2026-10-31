package compile

import (
	"context"
	"os"
	"strings"
	"testing"
)

func BenchmarkNormalizationCompileCancellation(b *testing.B) {
	sources := map[string]string{"attributes": `rule main = "a"` + strings.Repeat(` @error("label")`, 64)}
	for name, path := range map[string]string{"source-grammar": "../../zp.zp", "json": "../../parsers/json/json.zp"} {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		sources[name] = string(data)
	}
	for _, name := range []string{"json", "source-grammar", "attributes"} {
		source := sources[name]
		g, err := parseTest(source)
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
}
