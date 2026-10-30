package engine

import (
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
)

func TestHiddenRulesSpliceTheirExpression(t *testing.T) {
	t.Parallel()
	for _, tc := range gentest.SpliceCases() {
		t.Run(tc.Name, func(t *testing.T) {
			for _, opts := range []compile.Options{{}, {DefaultValue: true}, {SourceInfo: true}, {DisableMemo: true}} {
				hidden, inlined := compileBare(t, tc.Hidden, opts), compileBare(t, tc.Inlined, opts)
				matched := false
				for _, input := range tc.Inputs {
					if _, err := inlined.Parse("main", input); err == nil {
						matched = true
					}
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						want := gentest.ResultJSON(inlined.ParseWith("main", input, ParseOptions{Backend: backend}))
						got := gentest.ResultJSON(hidden.ParseWith("main", input, ParseOptions{Backend: backend}))
						if got != want {
							t.Errorf("%+v %q on %s:\nhidden:  %s\ninlined: %s", opts, input, backend, got, want)
						}
					}
				}
				if !matched {
					t.Fatal("no input is accepted")
				}
			}
		})
	}
}

// The elements of a hidden rule are the elements of its caller whatever the nesting.
func TestHiddenRulesSpliceInPlace(t *testing.T) {
	t.Parallel()
	check(t, `rule main = "a" _m "d"
rule _m = x:"b" "c"`, ok("abcd", `("a" "b" "c" "d")@main`))
	check(t, `rule main = _l
rule _l = "x" _l / "x"`, ok("xxx", `("x" ("x" "x"))`), fails("", `expected "x"`))
	check(t, `rule main = _e $$
rule _e = _e "+" "a" / "a"`, ok("a+a+a", `((("a" "+" "a") "+" "a"))@main`))
}

// A hidden rule is called as a rule: it is memoized, and it may recur.
func TestHiddenRulesAreCalledAsRules(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `rule main = _m "!" / _m "?"
rule _m = "a" "b"`)
	if got := result(p, "ab?"); got != `("a" "b" "?")@main` {
		t.Fatal(got)
	}
}

// A cut in a hidden rule commits the choices of the rule, not those around the call.
func TestHiddenRulesScopeTheirCuts(t *testing.T) {
	t.Parallel()
	check(t, `rule main = _m / "a" "c"
rule _m = "a" #cut "b"`, ok("ab", `("a" "b")@main`), ok("ac", `("a" "c")@main`))
	check(t, `rule main = ("a" #cut "b") / "a" "c"`, ok("ab", `("a" "b")@main`), fails("ac", `expected "b"`))
}
