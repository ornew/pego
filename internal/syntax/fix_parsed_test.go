package syntax_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/syntax"
)

// Fix rewrites positional captures to named captures that produce the same results. The engine
// no longer runs positional captures, so the rewrite is checked by its text, and by its results
// where the earlier source still runs.
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
		{"rule main = (\"a\" \"b\")* => map($1, (x) => $x)", "rule main = v1:(\"a\" \"b\")* => map($v1, (x) => x)", []string{"", "abab"}},
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
		{"type T struct { A Match }\nrule main T = \"a\" -- \"b\" => T{A: $2}", "type T = { A node[string] }\nrule main T = \"a\" #cut v2:\"b\" => T{A: $v2}", nil},
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
		pb := compileLegacy(t, tc.in, before)
		pa, err := engine.Compile(after, engine.Options{})
		if err != nil {
			t.Fatalf("%q: %v", got, err)
		}
		for _, input := range tc.inputs {
			if pb == nil {
				break
			}
			if b, a := run(pb, input), run(pa, input); b != a {
				t.Errorf("%q on %q:\nbefore %s\nafter  %s", got, input, b, a)
			}
		}
	}
}

// compileLegacy compiles g, parsed from the earlier syntax src. It returns nil when g uses a
// form that the value model removed (positional captures, a capture read as a field, startPos
// and endPos): such a grammar no longer runs, so the rewrite is checked by its text alone.
func compileLegacy(t *testing.T, src string, g *grammar.Grammar) *engine.Program {
	t.Helper()
	p, err := engine.Compile(g, engine.Options{})
	if err != nil {
		if msg := err.Error(); strings.Contains(msg, "is removed") || strings.Contains(msg, "read the capture with .$") {
			return nil
		}
		t.Fatalf("%q: %v", src, err)
	}
	return p
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

// Fix drops the $ of lambda parameters, and the results stay the same.
func TestFixLambdaParams(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		inputs   []string
	}{
		{"rule main = xs:\"a\"* => map($xs, (x) => $x)", "rule main = xs:\"a\"* => map($xs, (x) => x)", []string{"", "aa"}},
		{"rule main = xs:\"a\"* => foldl($xs, $xs, (acc, e) => list($acc, $e))", "rule main = xs:\"a\"* => foldl($xs, $xs, (acc, e) => list(acc, e))", []string{"a", "aaa"}},
		{"rule main = x:\"b\" xs:\"a\"* => list($x, map($xs, (x) => $x))", "rule main = x:\"b\" xs:\"a\"* => list($x, map($xs, (x) => x))", []string{"baa"}},
		{"rule main = xs:(i:\"a\")* => map($xs, (x) => map($xs, (y) => list($x, $y)))", "rule main = xs:(i:\"a\")* => map($xs, (x) => map($xs, (y) => list(x, y)))", []string{"aa"}},
		{"rule main = xs:\"a\"* => map($xs, (x) => x)", "rule main = xs:\"a\"* => map($xs, (x) => x)", []string{"aa"}},
		// A parameter named like a function: the call stays a call.
		{"rule main = xs:\"a\"* => filter($xs, (text) => text($text) == \"a\")", "rule main = xs:\"a\"* => filter($xs, (text) => text(text) == \"a\")", []string{"aa"}},
		// A capture read as a member is read with .$; fields and the node members stay.
		{"rule main = xs:(y:\"a\" \"b\")* => map($xs, (i) => i.y)", "rule main = xs:(y:\"a\" \"b\")* => map($xs, (i) => i.$y)", []string{"", "abab"}},
		{"rule main = xs:(y:\"a\" \"b\")+ => $xs[0].y", "rule main = xs:(y:\"a\" \"b\")+ => $xs[0].$y", []string{"abab"}},
		{"rule main = xs:(y:\"a\" \"b\")* => filter(map($xs, (i) => i.y), (j) => j.endPos > 1)", "rule main = xs:(y:\"a\" \"b\")* => filter(map($xs, (i) => i.$y), (j) => j.End > 1)", []string{"abab"}},
		{"rule main = x:\"a\" {$x.startPos == 0 && $x.endPos == 1}", "rule main = x:\"a\" {$x.Start == 0 && $x.End == 1}", []string{"a"}},
		{"type T = { A node[string] }\nrule main = x:t => $x.A\nrule t = a:\"a\" => T{A: $a}", "type T = { A node[string] }\nrule main = x:t => $x.A\nrule t = a:\"a\" => T{A: $a}", []string{"a"}},
		{"rule main = xs:(y:\"a\" \"b\")* => map($xs, (i) => i.$y)", "rule main = xs:(y:\"a\" \"b\")* => map($xs, (i) => i.$y)", []string{"ab"}},
		// Positional captures and parameters in one lambda.
		{"rule main = \"b\" xs:\"a\"* => map($2, (e) => list($1, $e))", "rule main = v1:\"b\" xs:\"a\"* => map($xs, (e) => list($v1, e))", []string{"baa"}},
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
		before, err := syntax.ParseLegacy(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		after, err := syntax.Parse(got)
		if err != nil {
			t.Fatalf("%q: %v", got, err)
		}
		pb := compileLegacy(t, tc.in, before)
		pa, err := engine.Compile(after, engine.Options{})
		if err != nil {
			t.Fatalf("%q: %v", got, err)
		}
		for _, input := range tc.inputs {
			if pb == nil {
				break
			}
			if b, a := run(pb, input), run(pa, input); b != a {
				t.Errorf("%q on %q:\nbefore %s\nafter  %s", got, input, b, a)
			}
		}
	}
}

// A lambda parameter written $x is rejected, and Fix refuses a lambda that mixes $x with a bare x,
// which referred to a variable.
func TestLambdaParamDollarRejected(t *testing.T) {
	_, err := syntax.Parse(`rule main = xs:"a"* => map($xs, (x) => $x)`)
	if err == nil || !strings.Contains(err.Error(), "1:40: lambda parameters are written without $: write x") {
		t.Fatalf("got %v", err)
	}
	if _, err := syntax.Fix(`rule main = {x := 1} xs:"a"* => map($xs, (x) => list($x, x))`); err == nil || !strings.Contains(err.Error(), "variable x would refer to the lambda parameter x") {
		t.Fatalf("mixed: %v", err)
	}
	if _, err := syntax.Parse(`rule main = xs:"a"* => map($xs, (nil) => nil)`); err == nil || !strings.Contains(err.Error(), "1:34: nil cannot name a lambda parameter") {
		t.Fatalf("keyword parameter: %v", err)
	}
	if _, err := syntax.Fix(`rule main = xs:"a"* => map($xs, (nil) => $nil)`); err == nil || !strings.Contains(err.Error(), "the lambda parameter nil would be read as the literal nil") {
		t.Fatalf("fix keyword parameter: %v", err)
	}
	// Source that does not parse keeps its other rewrites.
	if got, err := syntax.Fix("rule main = \"a\" -- (\n(x) => $x"); err != nil || got != "rule main = \"a\" #cut (\n(x) => $x" {
		t.Fatalf("unparsed: %q %v", got, err)
	}
}

// x.y with a name that is not a field name reads a capture, which is written x.$y.
func TestCaptureMemberRejected(t *testing.T) {
	_, err := syntax.Parse(`rule main = xs:(y:"a")* => map($xs, (i) => i.y)`)
	if err == nil || !strings.Contains(err.Error(), "1:46: a capture is read with .$y; run pego fix to rewrite .y") {
		t.Fatalf("got %v", err)
	}
	for _, src := range []string{
		`rule main = x:"a" => list($x.children)`,
		`rule main = x:"a" {$x.startPos == 0 && $x.endPos == 1}`,
		"type T = { A node[string] }\nrule main = x:t => $x.A\nrule t = a:\"a\" => T{A: $a}",
	} {
		if _, err := syntax.Parse(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

// A parameter is in scope in its lambda's body only, and an inner lambda's parameter hides an
// outer one of the same name.
func TestLambdaParamScope(t *testing.T) {
	for _, tc := range []struct {
		src    string
		params int
	}{
		{`rule main = xs:"a"* => list(map($xs, (x) => x), x)`, 1},
		{`rule main = xs:"a"* => map($xs, (x) => map($xs, (x) => x))`, 1},
		{`rule main = xs:"a"* => map($xs, (x) => list(map($xs, (y) => list(x, y)), x, y))`, 3},
	} {
		g, err := syntax.Parse(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		data, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(data), `"param":true`); n != tc.params {
			t.Errorf("%q: %d parameter references, want %d", tc.src, n, tc.params)
		}
	}
}
