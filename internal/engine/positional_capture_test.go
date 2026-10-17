package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// TestPositionalCaptureCompileErrors checks the diagnostics for positional captures. The grammar
// syntax no longer has them, but grammars loaded from JSON or compiled files still do.
func TestPositionalCaptureCompileErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`rule main = "a" "b" => $3`, `$3 is out of range: the rule has 2 elements with a value`},
		{`rule main = {x := $1} "a"
rule b = "b"`, `1:19: $1 cannot be used in predicates`},
		{`rule e = operators {
    operand "1"
    level { infix left "+" => $1 }
}`, `$1 cannot be used in operatorsRule operator actions`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			g, err := syntax.ParseLegacy(tc.src)
			if err != nil {
				t.Fatalf("syntax error: %v\n%s", err, tc.src)
			}
			_, err = Compile(g, Options{})
			if err == nil {
				t.Fatalf("expected a compile error\n%s", tc.src)
			}
			if got := err.Error(); !strings.Contains(got, tc.want) {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}
