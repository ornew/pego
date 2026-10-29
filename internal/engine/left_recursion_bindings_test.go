package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/limits"
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

// lrBindingSource re-enters a left-recursive rule with another variable environment: the
// binding before the left call changes k, which the rule reads. Only compiling it with
// leftRecursionBindings accepts it. plainLRSource is the same rule without the binding.
const (
	lrBindingSource = "var k int\n@start\nrule main = R $$\nrule R = {k := k + 1} R \"x\" / {k < 1000} \"x\""
	plainLRSource   = "var k int\n@start\nrule main = R $$\nrule R = R \"x\" / {k < 1000} \"x\""
)

// TestLeftRecursionGuardIgnoresVariables checks that a left-recursive re-entry at the same
// position gets the growing result even when its variable environment differs from that of
// the call being grown, on every backend and in Documents: the parse matches the same rule
// without the binding instead of nesting until the depth limit.
func TestLeftRecursionGuardIgnoresVariables(t *testing.T) {
	t.Parallel()
	inputs := []string{"x", "xxx", "xxxxxxxxxx", "", "y"}
	for _, options := range []compile.Options{{}, {DisableMemo: true}} {
		options.LeftRecursionBindings = true
		prog := mustCompile(t, lrBindingSource, options)
		plain := mustCompile(t, plainLRSource, options)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					// A step limit makes the parse take the controlled memo lookups.
					for _, limits := range []limits.WorkLimits{{}, {MaxSteps: 1 << 40}} {
						opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, MaxDepth: 400, WorkLimits: limits}
						for _, input := range inputs {
							got := resultJSON(prog.ParseWith("main", input, opts))
							want := resultJSON(plain.ParseWith("main", input, opts))
							if got != want {
								t.Fatalf("%s/%s recognize=%t DisableMemo=%t limits=%v %q:\n got  %s\n want %s", backend, unit, recognize, options.DisableMemo, limits, input, got, want)
							}
						}
					}
				}
				d, err := prog.NewDocumentWith("main", "xx", ParseOptions{Backend: backend, Unit: unit, MaxDepth: 400})
				if err != nil {
					t.Fatal(err)
				}
				for _, edit := range []struct {
					start, end int
					text       string
				}{{2, 2, "x"}, {0, 0, "xx"}, {1, 3, ""}, {0, 3, "y"}, {0, 1, "x"}} {
					if err := d.Edit(edit.start, edit.end, edit.text); err != nil {
						t.Fatal(err)
					}
					got := resultJSON(d.Parse())
					want := resultJSON(plain.ParseWith("main", d.Text(), ParseOptions{Backend: backend, Unit: unit}))
					if got != want {
						t.Fatalf("%s/%s Document %q:\n got  %s\n want %s", backend, unit, d.Text(), got, want)
					}
				}
			}
		}
	}
}
