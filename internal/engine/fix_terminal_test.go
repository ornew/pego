package engine

import (
	"fmt"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// A terminal type rewritten by pego fix holds the text the terminal held.
func TestFixTerminalTypes(t *testing.T) {
	for _, tc := range []struct{ src, input string }{
		{"type N terminal\nrule main N = \"-\"? [0-9]+", "-12"},
		{"type N terminal\ntype A ~ N\nrule main A = d:[0-9] [0-9]* {text($d) != \"0\"}", "12"},
		{"type N terminal\nrule main N = n => ...\nrule n N = [a-z]+", "ab"},
		{"type N terminal\nrule main N = x:n y:n {text($x) == text($y)} => N{text($x)}\nrule n N = [a-z]", "aa"},
	} {
		old, err := syntax.ParseLegacy(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		p, err := Compile(old, Options{})
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		want, err := p.Parse("main", tc.input)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		fixed, err := syntax.Fix(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		for _, opts := range []Options{{}, {NoTypeCheck: true}} {
			g, err := syntax.Parse(fixed)
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
			if text := fmt.Sprint(got.Field("Text")); text != want.Text() {
				t.Errorf("%q: Text is %q, want %q", fixed, text, want.Text())
			}
		}
	}
}
