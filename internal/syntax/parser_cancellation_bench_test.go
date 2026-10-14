package syntax

import (
	"context"
	"testing"
)

// BenchmarkParserCancellation compares ordinary parsing with the contextual
// partial parse used by editor analysis on identical grammar sources.
func BenchmarkParserCancellation(b *testing.B) {
	samples := sourceMetadataSamples(b)
	ctx := context.Background()
	for _, sample := range samples {
		b.Run("Parse/"+sample.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				g, err := Parse(sample.Source)
				if err != nil {
					b.Fatal(err)
				}
				sourceMetadataSink = g
			}
		})
		b.Run("ParsePartialContext/"+sample.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				g, diagnostics, err := ParsePartialContext(ctx, sample.Source)
				if err != nil {
					b.Fatal(err)
				}
				if len(diagnostics) > 0 {
					b.Fatal(diagnostics)
				}
				sourceMetadataSink = g
			}
		})
	}
}
