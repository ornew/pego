package compile

import (
	"strings"
	"testing"
)

func TestDiagnosticAttributeValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, want string }{
		{`rule main = "a" @diagnostic`, `requires exactly one named argument`},
		{`rule main = "a" @diagnostic("name")`, `requires exactly one named argument`},
		{`rule main = "a" @diagnostic(name="")`, `name must be nonempty`},
		{`rule main = "a" @diagnostic(name=1)`, `name must be a string`},
		{`rule main = "a" @diagnostic(name="a", extra="b")`, `requires exactly one named argument`},
		{`rule main = "a" @diagnostic(name="a") @diagnostic(name="b")`, `may occur only once`},
		{`rule main = "a" @diagnostic(name="a\nb")`, `contain no line separator`},
		{`rule main = "a" @diagnostic(name="a\u2028b")`, `contain no line separator`},
	} {
		t.Run(tc.source, func(t *testing.T) {
			if got := compileError(t, tc.source); !strings.Contains(got, tc.want) {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
