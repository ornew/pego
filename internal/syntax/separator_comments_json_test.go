package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// TestCommentsBeforeSemicolonJSON checks that a comment before a semicolon
// trails the statement the semicolon ends, so JSON keeps it.
func TestCommentsBeforeSemicolonJSON(t *testing.T) {
	src := `package p /* a */ ;
type T = { A int /* x */ ; B string /* y */ ; C int }
var v = 1 /* z */; rule r = "a" /* w */ ; rule ops = operators { operand "n" /* e */ ; level { infix left "+" /* f */ ; prefix "-" } /* g */ ; operand "m" }
`
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	out := grammar.Format(decoded)
	for _, c := range []string{"a", "x", "y", "z", "w", "e", "f", "g"} {
		if !strings.Contains(out, "/* "+c+" */") {
			t.Errorf("comment %q lost through JSON\n%s", c, out)
		}
	}
	// Each comment stays with its statement.
	for _, line := range []string{"B string /* y */", "var v = 1 /* z */", `rule r = "a" /* w */`, `operand "n" /* e */`, `infix left "+" /* f */`, "} /* g */"} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q\n%s", line, out)
		}
	}
}
