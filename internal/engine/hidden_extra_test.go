package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
)

// Captures made in a hidden rule are not visible to its caller, and those of the caller are not
// visible to it.
func TestHiddenRuleCapturesAreInvisible(t *testing.T) {
	t.Parallel()
	msg := compileError(t, `type R = { V string }
rule main = _m "c" => R{V: text($x)}
rule _m = x:"a" "b"`)
	if !strings.Contains(msg, "undefined capture $x") {
		t.Fatal(msg)
	}
	msg = compileError(t, `type R = { V string }
rule main = y:"a" _m => R{V: text($y)}
rule _m = "b" => R{V: text($y)}`)
	if !strings.Contains(msg, "undefined capture $y") {
		t.Fatal(msg)
	}
}

// The differential check of the splice cases detects a rule that makes its own node: the same
// grammars with the underscore removed from the name of the rule give another value, so the
// equality the check asserts is not vacuous.
func TestHiddenRulesNegativeControl(t *testing.T) {
	t.Parallel()
	differs := 0
	for _, tc := range gentest.SpliceCases() {
		if !strings.Contains(tc.Hidden, "rule _m") || strings.Contains(tc.Hidden, "_n") {
			continue
		}
		visible := strings.ReplaceAll(tc.Hidden, "_m", "m")
		hidden, shown, inlined := compileBare(t, tc.Hidden), compileBare(t, visible), compileBare(t, tc.Inlined)
		for _, input := range tc.Inputs {
			if _, err := inlined.Parse("main", input); err != nil {
				continue
			}
			want := resultJSON(inlined.Parse("main", input))
			if got := resultJSON(hidden.Parse("main", input)); got != want {
				t.Errorf("%s %q: hidden %s, inlined %s", tc.Name, input, got, want)
			}
			if resultJSON(shown.Parse("main", input)) != want {
				differs++
			}
		}
	}
	if differs == 0 {
		t.Fatal("a rule that makes its own node never differs from the grammar with its expression in place")
	}
}

// With default values every rule makes the default value of its expression. A hidden rule that had
// an action, or an inline action, still makes its own node then, as its action did: it does not
// become a rule that splices into its caller.
func TestDefaultValueKeepsTheNodeOfHiddenRulesWithActions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, actions, plain, spliced string }{
		{"inline default action", `rule main = "a" (x:"b" "c" => ...) "d"`, `rule main = "a" (x:"b" "c" => ...) "d"`, `rule main = "a" ("b" "c") "d"`},
		{"inline action", `rule main = "a" (x:"b" "c" => text($x)) "d"`, `rule main = "a" (x:"b" "c" => ...) "d"`, `rule main = "a" ("b" "c") "d"`},
		{"hidden rule with an action", "rule main = \"a\" _m \"d\"\nrule _m = x:\"b\" \"c\" => text($x)", "rule main = \"a\" _m \"d\"\nrule _m = x:\"b\" \"c\" => ...", `rule main = "a" ("b" "c") "d"`},
		{"hidden default action", "rule main = \"a\" _m \"d\"\nrule _m = x:\"b\" \"c\" => ...", "rule main = \"a\" _m \"d\"\nrule _m = x:\"b\" \"c\" => ...", `rule main = "a" ("b" "c") "d"`},
		{"nested", `rule main = "a" (x:"b" (y:"c" => text($y)) => text($x)) "d"`, `rule main = "a" (x:"b" (y:"c" => ...) => ...) "d"`, `rule main = "a" ("b" "c") "d"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			def, plain, spliced := compileBare(t, tc.actions, compile.Options{DefaultValue: true}), compileBare(t, tc.plain), compileBare(t, tc.spliced)
			differs := false
			for _, input := range []string{"abcd", "abd", "a", ""} {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					o := ParseOptions{Backend: backend}
					want, got := resultJSON(plain.ParseWith("main", input, o)), resultJSON(def.ParseWith("main", input, o))
					if got != want {
						t.Errorf("%q on %s:\ndefault value: %s\nas written:    %s", input, backend, got, want)
					}
					if resultJSON(spliced.ParseWith("main", input, o)) != want {
						differs = true
					}
				}
			}
			if !differs {
				t.Error("the node of the rule is never visible, so the case proves nothing")
			}
		})
	}
}
