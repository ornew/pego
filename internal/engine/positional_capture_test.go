package engine

import (
	"strings"
	"testing"
)

// TestPositionalCaptureCompileErrors checks the diagnostics for positional captures. The grammar
// syntax no longer has them, but grammars loaded from JSON or compiled files still do; they are
// removed in every context, since an element is named with a capture.
func TestPositionalCaptureCompileErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`rule main = "a" "b" => $3`, `1:24: positional capture $3 is removed; name the element with a capture`},
		{`rule main = {x := $1} "a"
rule b = "b"`, `1:19: positional capture $1 is removed; name the element with a capture`},
		{`rule e = #operators {
    operand "1"
    level { infix left "+" => $1 }
}`, `3:31: positional capture $1 is removed; name the element with a capture`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			g, err := parseLegacyTest(tc.src)
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
