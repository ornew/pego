package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/syntax"
)

// A terminal type rewritten by pego fix holds the text the terminal held: want is the text of
// the terminal that the grammar produced before.
func TestFixTerminalTypes(t *testing.T) {
	for _, tc := range []struct{ src, input, want string }{
		{"type N terminal\nrule main N = \"-\"? [0-9]+", "-12", "-12"},
		{"type N terminal\ntype A ~ N\nrule main A = d:[0-9] [0-9]* {text($d) != \"0\"}", "12", "12"},
		{"type N terminal\nrule main N = n => ...\nrule n N = [a-z]+", "ab", "ab"},
		{"type N terminal\nrule main N = x:n y:n {text($x) == text($y)} => N{text($x)}\nrule n N = [a-z]", "aa", "a"},
	} {
		fixed, err := syntax.Fix(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		for _, opts := range []compile.Options{{}} {
			g, err := parseTest(fixed)
			if err != nil {
				t.Fatalf("%q: %v", fixed, err)
			}
			p, err := Compile(g, opts)
			if err != nil {
				t.Fatalf("%q: %v", fixed, err)
			}
			got, err := p.Parse("main", tc.input)
			if err != nil {
				t.Fatalf("%q: %v", fixed, err)
			}
			// A terminal type becomes a named string node, or a struct { Text string } when
			// another constructor makes it.
			text := got.Text()
			if v, ok := got.Field("Text").(string); ok {
				text = v
			}
			if text != tc.want {
				t.Errorf("%q: Text is %q, want %q", fixed, text, tc.want)
			}
		}
	}
}
