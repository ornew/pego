package syntax

import (
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

// A comment before a value on the same line brings its own space, so the separator before it
// must not add a second one. The output must also format to itself.
func TestFormatCommentAfterSeparator(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"var x int /* a */ = /* b */ 1\n", "/* a */\nvar x int = /* b */ 1\n"},
		{"var x = /* b */ 1\n", "var x = /* b */ 1\n"},
		{"var x int = 1\n", "var x int = 1\n"},
		{"rule a = b => /* c */ 1\n", "rule a = b => /* c */ 1\n"},
		{"rule a = (b => /* c */ 1)\n", "rule a = (b => /* c */ 1)\n"},
	} {
		g, err := Parse(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		got := grammar.Format(g)
		if got != tc.want {
			t.Errorf("%q: got %q; want %q", tc.src, got, tc.want)
		}
		again, err := Parse(got)
		if err != nil {
			t.Fatalf("%q: %v", got, err)
		}
		if second := grammar.Format(again); second != got {
			t.Errorf("%q: formatting is not idempotent: %q", got, second)
		}
	}
}
