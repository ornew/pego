package compile

import (
	"strings"
	"testing"
)

// TestNamedNodeTypeErrors checks the type rules of named node types: a named node type is
// assignable to its underlying node type but not the reverse, distinct named node types are not
// assignable to each other, and a conversion (a rule's declared type or N{x}) needs a node whose
// underlying type fits.
func TestNamedNodeTypeErrors(t *testing.T) {
	t.Parallel()
	const defs = "type N = node[string]\ntype M = node[string]\ntype O = { A N; B node[string] }\ntype U ~ N | M\n"
	for _, tc := range []struct{ src, want string }{
		{`rule main O = x:"a" => O{A: $x}`, "cannot use node[string] as N in field A of O"},
		{`rule main O = x:m => O{A: $x}` + "\nrule m M = \"a\"", "cannot use M as N in field A of O"},
		{`rule main N = x:"a" => N{$x, $x}`, "a node type constructor takes one value: write N{x}"},
		{`rule main N = x:"a" => N{}`, "a node type constructor takes one value: write N{x}"},
		{`rule main O = x:"a" => O{$x}`, "a struct constructor names its fields: write O{Field: value}"},
		{`rule main N = "a" => N{1}`, "cannot convert int to N"},
		{`rule main N = "a" => N{"a"}`, "cannot convert string to N"},
		{`rule main N = "a" "b"`, "rule main produces node[(node[string], node[string])], which cannot be converted to N"},
		{`rule main N = x:"a" => O{A: N{$x}}`, "rule main produces O, which cannot be converted to N"},
		{`rule main U = "a"`, "rule main produces node[string], which is not assignable to N | M"},
		{`rule main N = x:("a" / "b" "c") => N{$x}`, "cannot convert node[string] | node[(node[string], node[string])] to N"},
		{`rule main = x:"a" => P{$x}`, "P is not a struct type or a node type"},
	} {
		if got := compileError(t, defs+tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("%s:\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
	for _, src := range []string{
		// N is assignable to node[string], and to a union containing it.
		`rule main O = x:n => O{A: $x, B: $x}` + "\nrule n N = \"a\"",
		`rule main U = n` + "\nrule n N = \"a\"",
		// A conversion takes any node whose underlying type fits, named or not.
		`rule main N = x:m => N{$x}` + "\nrule m M = \"a\"",
		`rule main N = m` + "\nrule m M = \"a\"",
		// Casts name the type.
		`rule main N = x:u => $x.(N)` + "\nrule u U = n\nrule n N = \"a\"",
	} {
		compileBare(t, defs+src)
	}
}
