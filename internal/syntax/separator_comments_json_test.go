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

// TestLaterCommentGroupsJSON checks that the groups that follow the trailing comment on its
// line, before a semicolon, a closing delimiter or the end of the file, trail the node too,
// in order, so JSON keeps them.
func TestLaterCommentGroupsJSON(t *testing.T) {
	src := `package p /* a1 */ /* a2 */ ;
type T = { A int /* x1 */ /* x2 */ ; B string /* y1 */ /* y2 */
}
rule r = "a" /* w1 */ /* w2 */ ; rule ops = operators { operand "n" /* e1 */ /* e2 */ ; level { infix left "+" /* f1 */ /* f2 */ } /* g1 */ /* g2 */
}
rule z = "z" /* z1 */ /* z2 */
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
	for _, c := range []string{"a1 ", "x1 ", "y1 ", "w1 ", "e1 ", "f1 ", "g1 ", "z1 "} {
		want := "/* " + c + "*/ /* " + c[:1] + "2 */"
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
}

// TestTrailingGroupsAcrossSemicolonJSON checks that a trailing group after a
// semicolon follows the groups before it instead of replacing them.
func TestTrailingGroupsAcrossSemicolonJSON(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"rule s = \"s\" /* s1 */ /* s2 */ ; // s3\n", " s1  s2  s3\n"},
		{"rule t = \"t\" /* t1 */ ; /* t2 */\n", " t1  t2 "},
		{"rule u = \"u\" /* u1 */ ; // u2\nrule v = \"v\"\n", " u1  u2\n"},
		{"type T = { A int /* a1 */ /* a2 */ ; /* a3 */\n}\n", " a1  a2  a3 "},
	} {
		g, err := Parse(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if r, ok := g.File.Decls[0].(*grammar.RuleDef); ok {
			got = strings.Join(r.TrailingComments, "")
		} else if d, ok := g.File.Decls[0].(*grammar.TypeDef); ok {
			got = strings.Join(d.Type.(*grammar.StructType).Fields[0].TrailingComments, "")
		}
		if got != tc.want {
			t.Errorf("%q: trailing %q, want %q", tc.src, got, tc.want)
		}
	}
}
