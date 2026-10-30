package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
	"github.com/ornew/pego/internal/limits"
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
		prog := mustCompile(t, gentest.LRBindingSource, options)
		plain := mustCompile(t, gentest.PlainLRSource, options)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, recognize := range []bool{false, true} {
					// A work limit makes the parse take the controlled memo lookups.
					for _, limits := range []limits.Limits{{}, {MaxWork: 1 << 40}} {
						opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, MaxDepth: 400, Limits: limits}
						for _, input := range inputs {
							got := gentest.ResultJSON(prog.ParseWith("main", input, opts))
							want := gentest.ResultJSON(plain.ParseWith("main", input, opts))
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
					got := gentest.ResultJSON(d.Parse())
					want := gentest.ResultJSON(plain.ParseWith("main", d.Text(), ParseOptions{Backend: backend, Unit: unit}))
					if got != want {
						t.Fatalf("%s/%s Document %q:\n got  %s\n want %s", backend, unit, d.Text(), got, want)
					}
				}
			}
		}
	}
}
