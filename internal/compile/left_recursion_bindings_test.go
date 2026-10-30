package compile

import (
	"strings"
	"testing"
)

// TestLeftRecursionBindingErrors checks which bindings in a left-recursion cycle are compile-time
// errors: those of a variable that the cycle reads, made before a left call into the cycle.
func TestLeftRecursionBindingErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"direct", "var k int\nrule main = R $$\nrule R = {k := k + 1} R \"x\" / \"x\"",
			`3:11: rule R binds variable k before its left-recursive call of R; the left recursion reads k`},
		{"constant", "var k int\nrule main = R $$\nrule R = {k := 1} R \"x\" / {k == 1} \"x\"",
			`3:11: rule R binds variable k before its left-recursive call of R; the left recursion reads k`},
		{"indirect", "var k int\nrule main = A $$\nrule A = B \"x\" / \"x\" {k > 0}\nrule B = {k := 2} A",
			`4:11: rule B binds variable k before its left-recursive call of A; the left recursion reads k`},
		{"read in a called rule", "var k int\nrule main = R $$\nrule R = {k := 1} R \"x\" / c\nrule c = {k == 0} \"x\"",
			`3:11: rule R binds variable k before its left-recursive call of R; the left recursion reads k`},
		{"nullable prefix", "var k int\nrule main = R $$\nrule R = ({k := 1} / \"y\")? \" \"* R \"x\" / \"x\" {k == 0}",
			`3:12: rule R binds variable k before its left-recursive call of R; the left recursion reads k`},
		{"choice alternative before the call", "var k int\nrule main = R $$\nrule R = ({k := 1} \"y\" / \"\") R \"x\" / \"x\" {k == 0}",
			`3:12: rule R binds variable k before its left-recursive call of R; the left recursion reads k`},
		{"lookahead", "var k int\nrule main = R $$\nrule R = &{k := 1} R \"x\" / \"x\" {k == 0}",
			`3:12: rule R binds variable k before its left-recursive call of R; the left recursion reads k`},
		{"lookahead after consumed input", "var k int\nrule main = r $$\nrule r = &(\"x\" {k := k + 1}) r \"x\" / {k == 0} \"x\"",
			`3:17: rule r binds variable k before its left-recursive call of r; the left recursion reads k`},
		{"nested lookahead", "var k int\nrule main = r $$\nrule r = &(&(\"y\" {k := 1}) \"z\") r \"x\" / {k == 0} \"x\"",
			`3:19: rule r binds variable k before its left-recursive call of r; the left recursion reads k`},
		{"block", "var k int\nvar j int\nrule main = R $$\nrule R = {j := 1; k := 2} R \"x\" / \"x\" {k == 0}",
			`4:19: rule R binds variable k before its left-recursive call of R; the left recursion reads k`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Fatalf("got %s\nwant %s", got, tc.want)
			}
			if got := compileError(t, tc.src); strings.Count(got, "binds variable") != 1 {
				t.Fatalf("want one error, got %s", got)
			}
		})
	}
	for _, tc := range []struct{ name, src string }{
		{"after the call", "var k int\nrule main = R $$\nrule R = R {k := k + 1} \"x\" {k > 0} / \"x\""},
		{"base alternative", "var k int\nrule main = R $$\nrule R = R \"x\" / {k := 1} \"x\" {k == 1}"},
		{"after consumed input", "var k int\nrule main = R $$\nrule R = R \"x\" / \"y\" {k := 1} R {k == 1}"},
		{"before the cycle", "var k int\nrule main = {k := 5} R $$\nrule R = R \"x\" {k == 5} / \"x\""},
		{"unread variable", "var k int\nrule main = R $$\nrule R = {k := 1} R \"x\" / \"x\"\nrule other = {k == 0}"},
		{"in a called rule", "var k int\nrule main = R $$\nrule R = b R \"x\" / \"x\" {k == 0}\nrule b = {k := 1}"},
		{"negative lookahead", "var k int\nrule main = r $$\nrule r = !({k := 1} \"y\") r \"x\" / {k == 0} \"x\""},
		{"call outside the cycle", "var k int\nrule main = R $$\nrule R = R \"x\" / {k := 1} c\nrule c = {k == 1} \"x\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustCompile(t, tc.src)
		})
	}
}
