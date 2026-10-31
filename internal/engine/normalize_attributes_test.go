package engine

import (
	"reflect"
	"testing"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/gentest"
)

func TestNormalizationCopyOnChangeParity(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		`rule main = "a"? (&"b" / !"x") "b"+ $$`,
		`rule main = ("a" #repeat(1, 3) #atomic @error("as")) "b" $$`,
		`rule main = ("a" #atomic ";") #recover(skip=([^;]+ ";")) $$`,
		`rule main = "a"* #stream $$`,
		`type Number = { Text (string) }
type Alias ~ (Number)
var count int
rule main (Alias) = {count := (1)} {(count == 1)} n:([0-9]+) => (Number{Text: (text($n))})`,
		`rule main = #operators {
    skip " "*
    operand [0-9]+
    operand "(" main ")"
    level { infix left "+" / "-" }
    level product { infix left "*" / "/" }
}`,
		`rule main = "a" @error(nil, "bad")`,
		`rule main = "a" @error("a", message="b")`,
		`rule main = "a" @atomic`,
		`rule main = "a" #repeat(3, 1)`,
	} {
		t.Run(source, func(t *testing.T) {
			g, err := parseTest(source)
			if err != nil {
				t.Fatal(err)
			}
			before, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			got, gotErr := compile.NormalizeAttributesWithCopies(g, false)
			want, wantErr := compile.NormalizeAttributesWithCopies(g, true)
			if !reflect.DeepEqual(gotErr, wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("normalization differs: got %#v / %v; want %#v / %v", got, gotErr, want, wantErr)
			}
			after, err := grammar.MarshalJSON(g)
			if err != nil || string(after) != string(before) {
				t.Fatalf("source changed: %v", err)
			}
			if gotErr != nil {
				return
			}
			optimized, optimizedErr := Compile(g, compile.Options{})
			eager, eagerErr := Compile(g, compile.Options{EagerAttributeCopies: true})
			if !reflect.DeepEqual(optimizedErr, eagerErr) {
				t.Fatalf("compile errors differ: %v / %v", optimizedErr, eagerErr)
			}
			if optimizedErr != nil {
				return
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					for _, input := range []string{"", "b", "ab", "aaab", "a;", "x;", "123", "1 + 2 * 3", "é"} {
						opts := ParseOptions{Backend: backend, Unit: unit}
						if actual, expected := gentest.ResultJSON(optimized.ParseWith("main", input, opts)), gentest.ResultJSON(eager.ParseWith("main", input, opts)); actual != expected {
							t.Fatalf("%s/%s %q: %s / %s", backend, unit, input, actual, expected)
						}
					}
				}
			}
			for _, p := range []*Program{optimized, eager} {
				_, _ = p.ParseWith("main", "ab", ParseOptions{Recognize: true})
			}
			after, err = grammar.MarshalJSON(g)
			if err != nil || string(after) != string(before) {
				t.Fatalf("compilation/backend initialization changed source: %v", err)
			}
		})
	}
}
