package gen_test

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/gen"
	"github.com/ornew/zeroparse/internal/gentest"
)

// TestNamedNodeTypeConversions runs conversions and casts across the backends: conversions keep
// the range and the value, a cast checks the type name, and a recursive node type reads its own
// elements.
func TestNamedNodeTypeConversions(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testNamedNodeTypeConversions)
}

func testNamedNodeTypeConversions(t *testing.T) {
	for _, tc := range []struct {
		src    string
		inputs map[string]string
	}{
		{`type N = node[string]
type M = node[string]
type U ~ N | M
rule main = x:u $$ => $x.(M)
rule u U = n / m
rule n N = "n"
rule m M = "m"`, map[string]string{
			"m": `{"node":{"type":"M","start":0,"end":1,"value":"m"}}`,
			"n": `{"err":"action in main: cannot cast N to M at input position 0 (line 1, column 1)%s","node":null}`,
		}},
	} {
		for _, sourceInfo := range []bool{false, true} {
			prog := compileBare(t, tc.src, compile.Options{SourceInfo: sourceInfo})
			for input, want := range tc.inputs {
				// With source information the error names the grammar position of the cast.
				suffix := ""
				if sourceInfo {
					suffix = "; cast at grammar position 4:26"
				}
				want = strings.Replace(want, "%s", suffix, 1)
				for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
					if got := gentest.ResultJSON(prog.ParseWith("main", input, engine.ParseOptions{Expectations: true, Backend: backend})); got != want {
						t.Errorf("%s %q:\n got %s\nwant %s", backend, input, got, want)
					}
				}
			}
		}
	}
	// A recursive node type.
	prog := compileBare(t, `type Tree = node[[]Tree]
type Out = { Size int; First *Tree }
rule main Out = t:tree $$ => Out{Size: len($t.Value), First: $t.Value[0]}
rule tree Tree = "(" ts:tree* ")" => Tree{$ts}`)
	for _, backend := range []engine.Backend{engine.Closure, engine.Bytecode, engine.BytecodeIterative} {
		got := gentest.ResultJSON(prog.ParseWith("main", "(()(()))", engine.ParseOptions{Expectations: true, Backend: backend}))
		if want := `"First":{"type":"Tree","start":2,"end":2,"value":[]},"Size":2`; !strings.Contains(got, want) {
			t.Errorf("%s: got %s, want %s", backend, got, want)
		}
	}
	// A conversion whose value does not fit the underlying type is a compile error. Without type
	// checking nothing checks it when the conversion runs.
	for _, tc := range []struct{ src, input, want string }{
		{`type N = node[string]
rule main N = x:("a" / "b" "c") => N{$x}`, "bc", "cannot convert node[string] | node[(node[string], node[string])] to N"},
		{`type N = node[string]
rule main N = "a" / "b" "c"`, "bc", "rule main produces node[string] | node[(node[string], node[string])], which cannot be converted to N"},
		// A conversion through an alias names the type itself, as Type() does.
		{`type N = node[string]
type A ~ N
rule main A = x:("a" / "b" "c") => A{$x}`, "bc", "cannot convert node[string] | node[(node[string], node[string])] to N"},
	} {
		if got := compileMessage(t, tc.src); !strings.Contains(got, tc.want) {
			t.Errorf("checked compilation: got %q, want an error containing %q", got, tc.want)
		}
	}
	// The generated parsers convert through an alias with the same name.
	g, err := parseTest(`type N = node[string]
type A ~ N
rule main A = x:"a" => A{$x}`)
	if err != nil {
		t.Fatal(err)
	}
	goCode, err := gentest.GenerateCode(t, g, gen.GenOptions{Package: "p", Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	tsCode, err := gentest.GenerateTSBundle(g, gen.GenOptions{Start: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goCode), `c.Convert("N", `) || !strings.Contains(string(tsCode), `c.convert("N", `) {
		t.Errorf("generated conversions do not name N")
	}
}
