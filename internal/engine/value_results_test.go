package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gentest"
)

func TestValueResultsAcrossBackends(t *testing.T) {
	t.Parallel()
	for _, fixture := range gentest.ValueResultCases() {
		t.Run(fixture.Name, func(t *testing.T) {
			program := mustCompile(t, fixture.Src)
			for _, input := range fixture.Inputs {
				baseline := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: Closure}))
				if strings.Contains(baseline, "error") {
					t.Fatalf("input=%q: %s", input, baseline)
				}
				for _, backend := range []Backend{Bytecode, BytecodeIterative} {
					if actual := gentest.ResultJSON(program.ParseWith("main", input, ParseOptions{Backend: backend})); actual != baseline {
						t.Fatalf("%s input=%q: %s / %s", backend, input, actual, baseline)
					}
				}
			}
		})
	}
}

func TestValueResults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, input, want string }{
		{"rule main = d:[0-9]+ #atomic $$ => len($d)", "123", "3"},
		{"rule main = xs:([a-z] #atomic)+ $$ => map($xs.Value, (x) => text(x))", "ab", `["a" "b"]`},
		{"rule main = \"a\" $$ => list(1, \"s\", true, nil)", "a", `[1 "s" true nil]`},
		{"rule main = \"a\" $$ => filter(list(1, 2, 3), (x) => x > 1)[1]", "a", "3"},
	} {
		n, err := mustCompile(t, tc.src).Parse("main", tc.input)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if got := n.String(); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.src, got, tc.want)
		}
	}
	// A function is still not a value.
	for _, src := range []string{
		"rule main = \"a\" => list((x) => x)",
		"rule main = \"a\" => (x) => x",
	} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Compile(g, compile.Options{}); err == nil {
			t.Errorf("%q: accepted", src)
		}
	}
}
