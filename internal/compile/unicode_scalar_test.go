package compile

import (
	"os"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

func BenchmarkCharacterRangeValidation(b *testing.B) {
	for _, tc := range []struct{ name, path string }{{"JSON", "../../parsers/json/json.zp"}, {"Go", "../../parsers/golang/golang.zp"}, {"YAML", "../../parsers/yaml/yaml.zp"}} {
		b.Run(tc.name, func(b *testing.B) {
			path := tc.path
			if tc.name == "YAML" && os.Getenv("ZP_BENCH_YAML_GRAMMAR") != "" {
				path = os.Getenv("ZP_BENCH_YAML_GRAMMAR")
			}
			source, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			g, err := parseTest(string(source))
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Validate", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := grammar.Validate(g); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Compile", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := compileStatic(g, Options{}, nil); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
