package compile

import (
	"strings"
	"testing"
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
