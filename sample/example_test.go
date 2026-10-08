package sample_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

const settings = `
def main = pair ("," " "? pair)*
def pair = key "=" value
def key = @(?a-z)+
def value = @(?0-9)+ / "\"" @(?^"\n)* "\"" / "true" / "false"
`

func ExampleGenerate() {
	p, err := pego.CompileSource(settings, "main")
	if err != nil {
		panic(err)
	}
	inputs, err := sample.Generate(p, 5, sample.WithSeed(1))
	if err != nil {
		panic(err)
	}
	for _, in := range inputs {
		fmt.Printf("%q\n", in)
	}
	// Output:
	// "h=\"\""
	// "in=true, i=true"
	// "utc=false"
	// "i=false"
	// "fjmv=false"
}

func ExampleGenerator_Coverage() {
	p, err := pego.CompileSource(settings, "main")
	if err != nil {
		panic(err)
	}
	// One input leaves some alternatives out; with WithCoverage, Generate would prefer them.
	g, err := sample.New(p, sample.WithSeed(1))
	if err != nil {
		panic(err)
	}
	if _, err := g.Generate(1); err != nil {
		panic(err)
	}
	fmt.Print(g.Coverage().Report())
	// Output:
	// rules 4/4 (100%), alternatives 1/4 (25%)
	// missed value: choice 0: @(?0-9)+
	// missed value: choice 2: "true"
	// missed value: choice 3: "false"
}

func ExampleGenerator_GenerateInvalid() {
	p, err := pego.CompileSource(settings, "main")
	if err != nil {
		panic(err)
	}
	g, err := sample.New(p, sample.WithSeed(1))
	if err != nil {
		panic(err)
	}
	invalid, err := g.GenerateInvalid(3)
	if err != nil {
		panic(err)
	}
	for _, inv := range invalid {
		fmt.Printf("%q (%s): %v\n", inv.Input, inv.Mutation, inv.Err)
	}
	// Output:
	// "h=\"\" " (insert " " at 4): 1:5: syntax error: expected ",", end of input
	// "n" (truncate at 1): 1:2: syntax error: expected "=", (?a-z)
	// "u=truee" (duplicate "e" at 5): 1:7: syntax error: expected ",", end of input
}

// FuzzCalculator checks that the calculator of examples/calculator never panics, whatever the input,
// that all backends agree, and that the generated inputs it is seeded with parse. Run it with
// go test -fuzz=FuzzCalculator ./sample; go test runs it on the seeds only.
func FuzzCalculator(f *testing.F) {
	src, err := os.ReadFile("../examples/calculator/calc.pego")
	if err != nil {
		f.Fatal(err)
	}
	p, err := pego.CompileSource(string(src), "main")
	if err != nil {
		f.Fatal(err)
	}
	// Valid inputs, and near-miss invalid ones for the error paths.
	valid := map[string]bool{}
	for _, in := range sample.Seed(f, p, 20, sample.WithMaxLen(64)) {
		valid[in] = true
	}
	g, err := sample.New(p, sample.WithSeed(1), sample.WithMaxLen(64))
	if err != nil {
		f.Fatal(err)
	}
	invalid, err := g.GenerateInvalid(10)
	if err != nil {
		f.Fatal(err)
	}
	for _, inv := range invalid {
		f.Add(inv.Input)
	}
	f.Fuzz(func(t *testing.T, in string) {
		n, err := p.Parse(in) // must not panic
		if valid[in] && err != nil {
			t.Fatalf("generated input %q does not parse: %v", in, err)
		}
		for _, b := range []pego.Backend{pego.Bytecode, pego.BytecodeIterative} {
			m, err2 := p.Parse(in, pego.WithBackend(b))
			if fmt.Sprint(n, err) != fmt.Sprint(m, err2) {
				t.Fatalf("%q: backend %v returned %v, %v; the closure backend %v, %v", in, b, m, err2, n, err)
			}
		}
	})
}
