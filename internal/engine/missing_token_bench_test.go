package engine

import (
	"errors"
	"testing"

	"github.com/ornew/pego/internal/value"
)

var missingBenchmarkNode *value.Node
var missingBenchmarkError error

// BenchmarkMissingLiteral separates the cost of declaring an insertion site
// from enabling its per-invocation ledger on the same valid input.
func BenchmarkMissingLiteral(b *testing.B) {
	for _, tc := range []struct {
		name, source, input string
		limit               int
		inserted            bool
	}{
		{"plain-valid", `rule main = "name" ";" $$`, "name;", 0, false},
		{"annotated-valid", `rule main = "name" ";" #missing $$`, "name;", 0, false},
		{"enabled-valid", `rule main = "name" ";" #missing $$`, "name;", 2, false},
		{"enabled-missing", `rule main = "name" ";" #missing $$`, "name", 2, true},
	} {
		g, err := parseTest(tc.source)
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, Options{})
		if err != nil {
			b.Fatal(err)
		}
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			b.Run(tc.name+"/"+unit.String(), func(b *testing.B) {
				o := ParseOptions{Unit: unit, MissingTokens: tc.limit}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					missingBenchmarkNode, missingBenchmarkError = prog.ParseWith("main", tc.input, o)
				}
				b.StopTimer()
				if missingBenchmarkNode == nil {
					b.Fatal("parse returned no tree")
				}
				var recovered SyntaxErrors
				if tc.inserted {
					if !errors.As(missingBenchmarkError, &recovered) || len(recovered) != 1 {
						b.Fatalf("missing diagnostic: %v", missingBenchmarkError)
					}
				} else if missingBenchmarkError != nil {
					b.Fatal(missingBenchmarkError)
				}
			})
		}
	}
}
