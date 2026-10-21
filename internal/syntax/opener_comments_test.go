package syntax

import "testing"

// A block comment right after an opening delimiter touches it: there is no
// space between them. The comment leads the token after it.
func TestOpenerBlockComments(t *testing.T) {
	const tail = "rule r = \"a\"\n"
	for _, tc := range []struct {
		name, in, want string
	}{
		{"call", "rule r = \"a\" => f( /* a */ 1, 2)\n", "rule r = \"a\" => f(/* a */ 1, 2)\n"},
		{"empty call", "rule r = \"a\" => f( /* a */ )\n", "rule r = \"a\" => f(/* a */)\n"},
		{"group", "rule r = ( /* a */ \"a\") \"b\"\n", "rule r = (/* a */ \"a\") \"b\"\n"},
		{"predicate", "rule r = \"a\" { /* a */ true } \"b\"\n", "rule r = \"a\" {/* a */ true} \"b\"\n"},
		{"index", "rule r = \"a\" => x[ /* a */ 0]\n", "rule r = \"a\" => x[/* a */ 0]\n"},
		{"struct type", "type T = { /* a */ F int }\n" + tail, "type T = { /* a */ F int }\n" + tail},
		{"empty struct type", "type T = { /* a */ }\n" + tail, "type T = {/* a */}\n" + tail},
		{"result type", "rule r node[ /* a */ int] = \"a\"\n", "rule r node[/* a */ int] = \"a\"\n"},
		{"attribute", "rule r = \"a\" @custom( /* a */ k=1)\n", "rule r = \"a\" @custom(/* a */ k=1)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// Comments inside an empty argument list of an attribute stay there.
func TestEmptyAttributeArgumentComments(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"block", "rule r = \"a\" @custom( /* a */ ) \"b\"\n", "rule r = \"a\" @custom(/* a */) \"b\"\n"},
		{"line", "rule r = \"a\" @custom(\n    // a\n) \"b\"\n", "rule r = \"a\" @custom(\n    // a\n) \"b\"\n"},
		{"none", "rule r = \"a\" @custom() \"b\"\n", "rule r = \"a\" @custom \"b\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// #repeat is written in the canonical spelling of its bounds, whichever the
// source used, and each comment stays before the token it was before, or moves
// to the next token that the canonical spelling has.
func TestRepeatPragmaSpellingAndComments(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"at least one", "rule r = \"a\" #repeat(1,) \"b\"\n", "rule r = \"a\"+ \"b\"\n"},
		{"at least zero", "rule r = \"a\" #repeat(0,) \"b\"\n", "rule r = \"a\"* \"b\"\n"},
		{"at least two", "rule r = \"a\" #repeat(2, ) \"b\"\n", "rule r = \"a\" #repeat(2,) \"b\"\n"},
		{"equal bounds", "rule r = \"a\" #repeat(2, 2) \"b\"\n", "rule r = \"a\" #repeat(2) \"b\"\n"},
		{"zero maximum", "rule r = \"a\" #repeat(,0) \"b\"\n", "rule r = \"a\" #repeat(0) \"b\"\n"},
		{"zero bounds", "rule r = \"a\" #repeat(0, 0) \"b\"\n", "rule r = \"a\" #repeat(0) \"b\"\n"},
		{"zero minimum", "rule r = \"a\" #repeat(0, 3) \"b\"\n", "rule r = \"a\" #repeat(,3) \"b\"\n"},
		{"no minimum", "rule r = \"a\" #repeat(,3) \"b\"\n", "rule r = \"a\" #repeat(,3) \"b\"\n"},
		{"both bounds", "rule r = \"a\" #repeat(2,3) \"b\"\n", "rule r = \"a\" #repeat(2, 3) \"b\"\n"},
		// With a comment between its tokens a repetition keeps its pragma.
		{"at least one with a comment", "rule r = \"a\" #repeat(1, /* a */) \"b\"\n", "rule r = \"a\" #repeat(1, /* a */) \"b\"\n"},
		{"at least zero with a comment", "rule r = \"a\" #repeat(/* a */ 0,) \"b\"\n", "rule r = \"a\" #repeat(/* a */ 0,) \"b\"\n"},
		// A token that the canonical spelling drops hands its comments to the next one.
		{"equal bounds with comments", "rule r = \"a\" #repeat(2 /* a */, /* b */ 2 /* c */) \"b\"\n", "rule r = \"a\" #repeat(2 /* a */ /* b */ /* c */) \"b\"\n"},
		{"zero maximum with a comment", "rule r = \"a\" #repeat(/* a */, /* b */ 0) \"b\"\n", "rule r = \"a\" #repeat(0 /* a */ /* b */) \"b\"\n"},
		{"zero minimum with a comment", "rule r = \"a\" #repeat(0 /* a */, 3) \"b\"\n", "rule r = \"a\" #repeat(/* a */,3) \"b\"\n"},
		{"after the parenthesis", "rule r = \"a\" #repeat( /* a */ 2) \"b\"\n", "rule r = \"a\" #repeat(/* a */ 2) \"b\"\n"},
		{"between the bounds", "rule r = \"a\" #repeat(2, /* a */ 3) \"b\"\n", "rule r = \"a\" #repeat(2, /* a */ 3) \"b\"\n"},
		{"before the comma", "rule r = \"a\" #repeat(2 /* a */, 3) \"b\"\n", "rule r = \"a\" #repeat(2 /* a */, 3) \"b\"\n"},
		{"before the closing parenthesis", "rule r = \"a\" #repeat(2, 3 /* a */) \"b\"\n", "rule r = \"a\" #repeat(2, 3 /* a */) \"b\"\n"},
		{"without a minimum", "rule r = \"a\" #repeat(, /* a */ 3) \"b\"\n", "rule r = \"a\" #repeat(, /* a */ 3) \"b\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.in); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
