package syntax

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

var sourceMetadataSink any

type sourceMetadataSample struct{ Name, Source string }

func sourceMetadataSamples(b *testing.B) []sourceMetadataSample {
	b.Helper()
	self, err := os.ReadFile("../../zp.zp")
	if err != nil {
		b.Fatal(err)
	}
	var comments strings.Builder
	var actionComments strings.Builder
	actionComments.WriteString("type Int = { Text string }\n")
	for i := 0; i < 64; i++ {
		fmt.Fprintf(&comments, "// detached %d\n\n// documentation %d\nrule r%d = \"x\" // trailing %d\n// detached after inline\n\n", i, i, i, i)
		fmt.Fprintf(&actionComments, "rule r%d = \"x\" => Int{/* value %d */ \"0\"}\n", i, i)
	}
	return []sourceMetadataSample{
		{"Small", `rule main = [a-z]+ #atomic`},
		{"Self", string(self)},
		{"Comments", comments.String()},
		{"ActionComments", actionComments.String()},
	}
}

// BenchmarkSourceMetadata measures syntax parsing, not parsing input with a
// compiled grammar. Comment-heavy input exercises owned documentation groups.
func BenchmarkSourceMetadata(b *testing.B) {
	for _, sample := range sourceMetadataSamples(b) {
		b.Run(sample.Name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				g, err := Parse(sample.Source)
				if err != nil {
					b.Fatal(err)
				}
				sourceMetadataSink = g
			}
		})
	}
}
