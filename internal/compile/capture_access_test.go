package compile

import (
	"strings"
	"testing"
)

func TestCaptureAccessErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{`rule main = x:(y:"a" "b") => $x.$z`, "has no capture z"},
		{"type T = { A node[string] }\nrule main = x:t => $x.$A\nrule t = a:\"a\" => T{A: $a}", "T has no capture A"},
		{`rule main = x:(y:"a" "b")? => $x.$y`, "the value may be nil"},
		{`rule main = "a" => (1).$y`, "int has no capture y"},
		{`rule main = x:((y:"a" "b") / (z:"c" "d")) => $x.$y`, "has no capture y"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if _, err := compileStatic(g, Options{}, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// A struct value has no captures.
	if got := compileMessage(t, "type T = { A node[string] }\nrule main = x:t => $x.$A\nrule t = a:\"a\" => T{A: $a}"); !strings.Contains(got, "T has no capture A") {
		t.Fatalf("got %q", got)
	}
}
