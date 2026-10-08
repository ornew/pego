package cel_test

import (
	"testing"

	"github.com/ornew/pego/parsers/cel"
)

// TestMacros checks which calls CheckMacros rejects (the differential test compares it with cel-go on 12,000
// expressions; these are the rules, one by one).
func TestMacros(t *testing.T) {
	for _, tc := range []struct {
		src string
		ok  bool
	}{
		{"has(a.b)", true},
		{"has((a.b))", true},
		{"has(a.b.c)", true},
		{"has({}.b)", true},
		{"has(has(a.b))", true}, // the inner has expands to a selection
		{"has(a)", false},
		{"has(a[0])", false},
		{"has(a.?b)", false},
		{"has(a.b())", false},
		{"has(1)", false},
		{"has(a.b, c)", true}, // not the macro: it takes one argument
		{"has()", true},
		{".has(a)", true},
		{"a.has(b)", true}, // a method, not the macro
		{"a.all(x, x > 0)", true},
		{"a.exists(x, x)", true},
		{"a.exists_one(x, x)", true},
		{"a.existsOne(x, x)", true},
		{"a.map(x, x * 2)", true},
		{"a.map(x, x > 1, x * 2)", true},
		{"a.filter(x, x)", true},
		{"a.all((x), x)", true},
		{"a.all(.x, x)", true},
		{"a.all(a.b, x)", false},
		{"a.all(1, x)", false},
		{"a.exists('x', x)", false},
		{"a.map(x.y, x)", false},
		{"a.filter(f(x), x)", false},
		{"a.all(__result__, x)", false},
		{"a.all(.__result__, x)", true},
		{"a.map(x, y, z, w)", true}, // four arguments: not the macro
		{"a.filter(1, 2, 3)", true},
		{"a.all(1)", true},
		{"all(1, 2)", true}, // a global call, not the macro
		{"a.optMap(1, 2)", true},
		{"a.all(x, a.all(1, y))", false}, // the nested macro is checked as well
	} {
		e, err := cel.ParseExpr(tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if got := cel.CheckMacros(e) == nil; got != tc.ok {
			t.Errorf("CheckMacros(%s) = %v, want %v", tc.src, cel.CheckMacros(e), tc.ok)
		}
		_, err = cel.ParseExpr(tc.src, cel.WithMacros())
		if (err == nil) != tc.ok {
			t.Errorf("ParseExpr(%s, WithMacros) = %v, want ok = %v", tc.src, err, tc.ok)
		}
	}
}
