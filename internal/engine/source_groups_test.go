package engine

import (
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

func TestSourceGroupsExecutableParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testSourceGroupsExecutableParity)
}

func testSourceGroupsExecutableParity(t *testing.T) {
	const grouped = `type Number = { Text string }
type Alias ~ (Number)
type Value = { Number (Alias); Items ([]Alias); Flag (bool) }
var count int
rule main (Value) = {count := (1 + (2 * 3))} {((count == 7) && true)} n:number => (Value{Number: ($n), Items: (map(list($n), (item) => (item))), Flag: (count == 7)})
rule number (Alias) = v1:[0-9]+ => (Number{Text: (text($v1))})
`
	const plain = `type Number = { Text string }
type Alias ~ Number
type Value = { Number Alias; Items []Alias; Flag bool }
var count int
rule main Value = {count := 1 + 2 * 3} {count == 7 && true} n:number => Value{Number: $n, Items: map(list($n), (item) => item), Flag: count == 7}
rule number Alias = v1:[0-9]+ => Number{Text: text($v1)}
`
	g, err := parseTest(grouped)
	if err != nil {
		t.Fatal(err)
	}
	before, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	ref := mustCompile(t, plain)
	for _, fromJSON := range []bool{false, true} {
		input := g
		if fromJSON {
			input, err = grammar.DecodeJSON(before)
			if err != nil {
				t.Fatal(err)
			}
		}
		p, err := Compile(input, compile.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				for _, source := range []string{"123", "0", "", "x"} {
					opts := ParseOptions{Backend: backend, Unit: unit}
					if got, want := gentest.ResultJSON(p.ParseWith("main", source, opts)), gentest.ResultJSON(ref.ParseWith("main", source, opts)); got != want {
						t.Fatalf("JSON=%v %s/%s %q: got %s want %s", fromJSON, backend, unit, source, got, want)
					}
				}
			}
		}
		// Generation consumes the normalized grammar for both typed and
		// untyped targets; source grouping must not change generated code.
		for _, typed := range []bool{false, true} {
			opts := gen.GenOptions{Package: "sample", Start: "main", Types: typed}
			plainAST, err := parseTest(plain)
			if err != nil {
				t.Fatal(err)
			}
			for _, generate := range []func(*grammar.Grammar, gen.GenOptions) ([]byte, error){func(g *grammar.Grammar, o gen.GenOptions) ([]byte, error) { return gentest.GenerateCode(t, g, o) }, gentest.GenerateTSBundle} {
				got, err := generate(input, opts)
				if err != nil {
					t.Fatal(err)
				}
				want, err := generate(plainAST, opts)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Fatalf("generated code changed, typed=%v JSON=%v", typed, fromJSON)
				}
			}
		}
		unchanged, err := grammar.MarshalJSON(input)
		if err != nil || string(unchanged) != string(before) {
			t.Fatalf("source AST mutated: %v", err)
		}
	}
}
