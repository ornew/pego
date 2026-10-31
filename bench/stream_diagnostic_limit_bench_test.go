package bench

import (
	"errors"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

// BenchmarkStreamDiagnosticLimit compares complete and bounded recovery
// results for the same 1,000-error stream. Element delivery stays enabled in
// both modes so each recovery is committed at the same stream boundary.
func BenchmarkStreamDiagnosticLimit(b *testing.B) {
	p, err := zp.CompileSource(`@start
rule main = item+ #stream $$
rule item = "ab" #recover(skip=[^a]+)`, "main")
	if err != nil {
		b.Fatal(err)
	}
	const count = 1000
	input := strings.Repeat("abX!", count)
	for _, be := range backends {
		for _, unit := range units {
			for _, mode := range []string{"complete", "limit-32"} {
				b.Run(be.name+"/"+unit.name+"/"+mode, func(b *testing.B) {
					opts := []zp.ParseOption{zp.WithBackend(be.b), zp.WithUnit(unit.u)}
					if mode == "limit-32" {
						opts = append(opts, zp.WithDiagnosticLimit(32))
					}
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						elements := 0
						err := p.ParseStream(strings.NewReader(input), func(*zp.Node) error {
							elements++
							return nil
						}, opts...)
						if elements != 2*count {
							b.Fatalf("delivered %d elements, want %d", elements, 2*count)
						}
						var recovered zp.SyntaxErrors
						if !errors.As(err, &recovered) {
							b.Fatalf("missing recovered errors: %v", err)
						}
						if mode == "complete" && len(recovered) != count {
							b.Fatalf("complete result has %d errors", len(recovered))
						}
						if mode == "limit-32" {
							var truncated *zp.TruncatedSyntaxErrors
							if !errors.As(err, &truncated) || truncated.Total != count || len(recovered) != 32 {
								b.Fatalf("bounded result: %v", err)
							}
						}
					}
				})
			}
		}
	}
}
