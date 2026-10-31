package cel_test

import (
	"regexp"
	"testing"

	"github.com/ornew/zeroparse/parsers/cel"
	"github.com/ornew/zeroparse/parsers/cel/types"
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
	id := func(s string) *types.Ident { return &types.Ident{TNode: types.TNode[string]{Value: s}} }
	op := func(s string) *types.TNode[string] { return &types.TNode[string]{Value: s} }
	for _, tc := range []struct {
		e    types.Expr
		want string
	}{
		{&types.Binary{Left: &types.Binary{Left: id("a"), Op: op("+"), Right: id("b")}, Op: op("*"), Right: id("c")}, "(a + b) * c"},
		{&types.Binary{Left: id("a"), Op: op("-"), Right: &types.Binary{Left: id("b"), Op: op("-"), Right: id("c")}}, "a - (b - c)"},
		{&types.Binary{Left: &types.Binary{Left: id("a"), Op: op("-"), Right: id("b")}, Op: op("-"), Right: id("c")}, "a - b - c"},
		{&types.Select{X: &types.Unary{Op: op("-"), X: id("a")}, Field: &types.Name{TNode: types.TNode[string]{Value: "f"}}}, "(-a).f"},
		{&types.Unary{Op: op("!"), X: &types.Binary{Left: id("a"), Op: op("||"), Right: id("b")}}, "!(a || b)"},
		{&types.Unary{Op: op("-"), X: &types.IntLit{TNode: types.TNode[string]{Value: "-1"}}}, "-(-1)"},
		{&types.Unary{Op: op("!"), X: &types.Unary{Op: op("!"), X: id("a")}}, "!!a"},
		{&types.Conditional{Cond: &types.Conditional{Cond: id("a"), Then: id("b"), Else: id("c")}, Then: id("d"), Else: id("e")}, "(a ? b : c) ? d : e"},
		{&types.Conditional{Cond: id("a"), Then: &types.Conditional{Cond: id("b"), Then: id("c"), Else: id("d")}, Else: id("e")}, "a ? (b ? c : d) : e"},
		{&types.Conditional{Cond: id("a"), Then: id("b"), Else: &types.Conditional{Cond: id("c"), Then: id("d"), Else: id("e")}}, "a ? b : c ? d : e"},
		{&types.Binary{Left: id("a"), Op: op("in"), Right: &types.ListLit{Elems: []types.Expr{id("b"), &types.Optional{X: id("c")}}}}, "a in [b, ?c]"},
	} {
		if got := cel.Format(tc.e); got != tc.want {
			t.Errorf("Format = %q, want %q", got, tc.want)
		}
	}
}
