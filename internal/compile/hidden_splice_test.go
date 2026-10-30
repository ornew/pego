package compile

import (
	"strings"
	"testing"
)

// A hidden rule whose elements contain themselves has no fixed number of them.
func TestHiddenRulesCannotContainThemselvesFlat(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"rule main = _a\nrule _a = \"x\" _a",
		"rule main = _a\nrule _a = \"x\" _b\nrule _b = _a \"y\"",
	} {
		msg := compileError(t, src)
		if !strings.Contains(msg, "contains itself") {
			t.Errorf("%s: %s", src, msg)
		}
	}
}
