package compile

import (
	"strings"
	"testing"
)

// The declared node[T] and tuple types of fields and rules are checked when the grammar is
// compiled.
func TestNodeTypesCheckedAtCompileTime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, static, input string }{
		{"type P = { V node[[]node[string]] }\nrule main P = v:(\"a\" \"b\") => P{V: $v}", "cannot use", "ab"},
		{"type P = { V node[(node[string], node[string], node[string])] }\nrule main P = v:(\"a\" \"b\") => P{V: $v}", "cannot use", "ab"},
		{"type P = { T (node[string],) }\nrule main P = v:(\"a\" \"b\") => P{T: $v.Value}", "cannot use", "ab"},
		{"rule main node[[]node[string]] = \"a\" \"b\"", "not assignable", "ab"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), tc.static) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.static)
		}
	}
}
