package cel_test

import (
	"regexp"
	"testing"

	"github.com/ornew/pego/parsers/cel"
)

// stripPos removes the offsets from a canonical form.
var stripPos = regexp.MustCompile(`@[0-9]+`)

// TestFormat checks that Format prints every accepted expression of the reference corpora as text that parses to the
// same expression (without the positions), and that formatting is stable.
func TestFormat(t *testing.T) {
	nolimits := cel.WithLimits(cel.Limits{})
	n := 0
	for _, src := range refSources(t) {
		e, err := cel.ParseExpr(src, nolimits)
		if err != nil {
			continue
		}
		n++
		out := cel.Format(e)
		e2, err := cel.ParseExpr(out, nolimits)
		if err != nil {
			t.Fatalf("%q formatted as %q: %v", src, out, err)
		}
		if a, b := stripPos.ReplaceAllString(canonical(src, e), ""), stripPos.ReplaceAllString(canonical(out, e2), ""); a != b {
			t.Fatalf("%q formatted as %q\n got  %s\n want %s", src, out, b, a)
		}
		if out2 := cel.Format(e2); out2 != out {
			t.Fatalf("%q formatted as %q and then as %q", src, out, out2)
		}
	}
	t.Logf("%d expressions", n)
}

// TestFormatBuilt checks the parentheses that Format adds to trees built without Paren nodes.
func TestFormatBuilt(t *testing.T) {
	id := func(s string) *cel.Ident { return &cel.Ident{Text: s} }
	op := func(s string) *cel.Match { return &cel.Match{Text: s} }
	for _, tc := range []struct {
		e    cel.Expr
		want string
	}{
		{&cel.Binary{Left: &cel.Binary{Left: id("a"), Op: op("+"), Right: id("b")}, Op: op("*"), Right: id("c")}, "(a + b) * c"},
		{&cel.Binary{Left: id("a"), Op: op("-"), Right: &cel.Binary{Left: id("b"), Op: op("-"), Right: id("c")}}, "a - (b - c)"},
		{&cel.Binary{Left: &cel.Binary{Left: id("a"), Op: op("-"), Right: id("b")}, Op: op("-"), Right: id("c")}, "a - b - c"},
		{&cel.Select{X: &cel.Unary{Op: op("-"), X: id("a")}, Field: &cel.Name{Text: "f"}}, "(-a).f"},
		{&cel.Unary{Op: op("!"), X: &cel.Binary{Left: id("a"), Op: op("||"), Right: id("b")}}, "!(a || b)"},
		{&cel.Unary{Op: op("-"), X: &cel.IntLit{Text: "-1"}}, "-(-1)"},
		{&cel.Unary{Op: op("!"), X: &cel.Unary{Op: op("!"), X: id("a")}}, "!!a"},
		{&cel.Conditional{Cond: &cel.Conditional{Cond: id("a"), Then: id("b"), Else: id("c")}, Then: id("d"), Else: id("e")}, "(a ? b : c) ? d : e"},
		{&cel.Conditional{Cond: id("a"), Then: &cel.Conditional{Cond: id("b"), Then: id("c"), Else: id("d")}, Else: id("e")}, "a ? (b ? c : d) : e"},
		{&cel.Conditional{Cond: id("a"), Then: id("b"), Else: &cel.Conditional{Cond: id("c"), Then: id("d"), Else: id("e")}}, "a ? b : c ? d : e"},
		{&cel.Binary{Left: id("a"), Op: op("in"), Right: &cel.ListLit{Elems: []cel.Expr{id("b"), &cel.Optional{X: id("c")}}}}, "a in [b, ?c]"},
	} {
		if got := cel.Format(tc.e); got != tc.want {
			t.Errorf("Format = %q, want %q", got, tc.want)
		}
	}
}
