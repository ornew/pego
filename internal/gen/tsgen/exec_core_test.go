package tsgen

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/ir/exec/core"
)

// TestSubstituteMethods checks that the printed compositions replace the hand-written methods of
// the runtime, with the comment lines before them, and nothing else.
func TestSubstituteMethods(t *testing.T) {
	ms, err := core.TSMethods()
	if err != nil {
		t.Fatal(err)
	}
	got := substituteMethods(tsRuntimeSource, ms)
	want := tsRuntimeSource
	for _, m := range ms {
		if strings.Count(got, m.Text) != 1 {
			t.Fatalf("the printed method %s is not in the runtime once", m.Name)
		}
		got = strings.Replace(got, m.Text, "<"+m.Name+">", 1)
		// The hand-written method: its comment lines, its first line and its closing brace.
		i := strings.Index(want, "\n  "+m.Name+"(") + 1
		start := i
		for {
			j := strings.LastIndex(want[:start-1], "\n") + 1
			if !strings.HasPrefix(want[j:], "  // ") {
				break
			}
			start = j
		}
		end := i + strings.Index(want[i:], "\n  }\n") + len("\n  }\n")
		want = want[:start] + "<" + m.Name + ">" + want[end:]
	}
	if got != want {
		t.Error("the substitution changed the runtime outside the replaced methods")
	}
	if strings.Contains(got, "  callBody(") || strings.Contains(got, "  grow(") {
		t.Error("a hand-written method is left")
	}
}
