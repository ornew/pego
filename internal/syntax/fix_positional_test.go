package syntax_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

// Fix rewrites positional captures to named captures that produce the same results.
func TestFixPositional(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		inputs   []string
	}{
		{"rule main = \"a\" b => $2\nrule b = \"b\"", "rule main = \"a\" b:b => $b\nrule b = \"b\"", []string{"ab", "a"}},
		{"rule main = -\"x\" &\"a\" \"a\" -\"b\" \"c\" => list($2, $1)", "rule main = -\"x\" &\"a\" v1:\"a\" -\"b\" v2:\"c\" => list($v2, $v1)", []string{"xabc"}},
		{"rule main = n:\"a\" \"b\" => list($1, $2, $n)", "rule main = n:\"a\" v2:\"b\" => list($n, $v2, $n)", []string{"ab"}},
		{"rule main = \"a\" \"b\"? => $0", "rule main = v1:\"a\" v2:\"b\"? => list($v1, $v2)", []string{"a", "ab"}},
		{"rule main = \"a\" / \"b\" => $1", "rule main = v1:(\"a\" / \"b\") => $v1", []string{"a", "b"}},
		{"rule main = \"a\" => list($0, $1)", "rule main = v1:\"a\" => list(list($v1), $v1)", []string{"a"}},
		{"rule main = v1:\"a\" \"b\" => $2", "rule main = v1:\"a\" v2:\"b\" => $v2", []string{"ab"}},
		{"rule main = b \"x\" b => list($1, $3)\nrule b = \"b\"", "rule main = b:b \"x\" v3:b => list($b, $v3)\nrule b = \"b\"", []string{"bxb"}},
		{"rule main = (\"a\" \"b\")* => map($1, (x) => $x)", "rule main = v1:(\"a\" \"b\")* => map($v1, (x) => $x)", []string{"", "abab"}},
		{"rule main = \"a\" #recover(skip=[^;]*) \";\" => $1", "rule main = v1:\"a\" #recover(skip=[^;]*) \";\" => $v1", []string{"a;", "z;"}},
		{"rule main = (\"a\" \"b\") => $1", "rule main = (v1:\"a\" \"b\") => $v1", []string{"ab"}},
		{"rule main = (\n    \"a\"\n    \"b\"\n) => $2", "rule main = (\n    \"a\"\n    v2:\"b\"\n) => $v2", []string{"ab"}},
		{"rule main = ((\"a\" \"b\" \"c\")) => $3", "rule main = ((\"a\" \"b\" v3:\"c\")) => $v3", []string{"abc"}},
		{"rule main = \"a\" (&\"b\") \"b\" \"c\" => $3", "rule main = \"a\" (&\"b\") \"b\" v3:\"c\" => $v3", []string{"abc"}},
		{"rule main = \"a\" (#cut) \"b\" => $0", "rule main = v1:\"a\" (#cut) v2:\"b\" => list($v1, $v2)", []string{"ab", "a"}},
		{"rule main = \"a\" (\"b\" / \"c\") => $2", "rule main = \"a\" v2:(\"b\" / \"c\") => $v2", []string{"ab", "ac"}},
		{"rule main = x y => map($2, (x) => $1)\nrule x = \"x\"\nrule y = \"y\"*", "rule main = v1:x y:y => map($y, (x) => $v1)\nrule x = \"x\"\nrule y = \"y\"*", []string{"xyy"}},
		{"rule main = (x:\"a\") \"b\" => list($1, $0)", "rule main = (x:\"a\") v2:\"b\" => list($x, list($x, $v2))", []string{"ab"}},
		{"rule main = (b) \"x\" => $1\nrule b = \"b\"", "rule main = b:(b) \"x\" => $b\nrule b = \"b\"", []string{"bx"}},
		{"rule main = \"é\" \"b\" => $2", "rule main = \"é\" v2:\"b\" => $v2", []string{"éb"}},
		{"type T struct { A Match }\nrule main T = \"a\" -- \"b\" => T{A: $2}", "type T = { A Match }\nrule main T = \"a\" #cut v2:\"b\" => T{A: $v2}", nil},
		{"rule main = operators {\n    operand \"(\" main \")\" => $2\n    operand [0-9]\n    level { infix left \"+\" => list($lhs, $rhs) }\n}", "rule main = operators {\n    operand \"(\" main:main \")\" => $main\n    operand [0-9]\n    level { infix left \"+\" => list($lhs, $rhs) }\n}", []string{"1+(2+3)"}},
	} {
		got, err := syntax.Fix(tc.in)
		if err != nil {
			t.Errorf("Fix(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Fix(%q)\ngot  %q\nwant %q", tc.in, got, tc.want)
			continue
		}
		if again, err := syntax.Fix(got); err != nil || again != got {
			t.Errorf("Fix is not idempotent on %q: %q %v", got, again, err)
		}
		if tc.inputs == nil {
			// The input also has syntax that only the token rewrites accept.
			if _, err := syntax.Parse(got); err != nil {
				t.Errorf("%q: %v", got, err)
			}
			continue
		}
		before, err := syntax.ParseLegacy(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		after, err := syntax.Parse(got)
		if err != nil {
			t.Fatalf("%q: %v", got, err)
		}
		pb, err := engine.Compile(before, engine.Options{})
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		pa, err := engine.Compile(after, engine.Options{})
		if err != nil {
			t.Fatalf("%q: %v", got, err)
		}
		for _, input := range tc.inputs {
			if b, a := run(pb, input), run(pa, input); b != a {
				t.Errorf("%q on %q:\nbefore %s\nafter  %s", got, input, b, a)
			}
		}
	}
}

func run(p *engine.Program, input string) string {
	n, err := p.ParseWith("main", input, engine.ParseOptions{})
	if err != nil {
		return "error: " + err.Error()
	}
	return n.String()
}

// Positional captures are rejected; Fix reports out-of-range ones and refuses source that does
// not parse.
func TestPositionalRejected(t *testing.T) {
	_, err := syntax.Parse(`rule main = "a" => $1`)
	if err == nil || !strings.Contains(err.Error(), "pego fix") {
		t.Fatalf("got %v", err)
	}
	if _, err := syntax.Fix(`rule main = "a" => $2`); err == nil || !strings.Contains(err.Error(), "1:20: $2 is out of range") {
		t.Fatalf("out of range: %v", err)
	}
	if _, err := syntax.Fix(`rule main = "a" {$1 != nil} => $1`); err == nil || !strings.Contains(err.Error(), "1:18: $1 cannot be rewritten") {
		t.Fatalf("predicate: %v", err)
	}
	if _, err := syntax.Fix(`rule main = ( => $1`); err == nil {
		t.Fatal("Fix accepted positional captures in source that does not parse")
	}
}
