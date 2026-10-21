package engine

import (
	"strings"
	"testing"
)

// Captures made in a hidden rule are not visible to its caller, and those of the caller are not
// visible to it.
func TestHiddenRuleCapturesAreInvisible(t *testing.T) {
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
	differs := 0
	for _, tc := range spliceCases() {
		if !strings.Contains(tc.hidden, "rule _m") || strings.Contains(tc.hidden, "_n") {
			continue
		}
		visible := strings.ReplaceAll(tc.hidden, "_m", "m")
		hidden, shown, inlined := compileBare(t, tc.hidden), compileBare(t, visible), compileBare(t, tc.inlined)
		for _, input := range tc.inputs {
			if _, err := inlined.Parse("main", input); err != nil {
				continue
			}
			want := resultJSON(inlined.Parse("main", input))
			if got := resultJSON(hidden.Parse("main", input)); got != want {
				t.Errorf("%s %q: hidden %s, inlined %s", tc.name, input, got, want)
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
