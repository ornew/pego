package engine_test

import (
	"errors"
	"testing"

	"github.com/ornew/zeroparse"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/starttest"
	"github.com/ornew/zeroparse/sample"
)

// TestSampleCorpus generates inputs for every grammar of the test corpus with package sample and checks
// that each one parses, without errors, on every backend. The corpus covers predicates, variables,
// lookahead, #recover, Operators Rule expressions, left recursion, anchors and character classes.
func TestSampleCorpus(t *testing.T) {
	t.Parallel()
	for _, c := range gentest.GenCorpus(t) {
		t.Run(c.Name, func(t *testing.T) {
			ast, err := zeroparse.ParseGrammar(c.Src)
			if err != nil {
				t.Fatal(err)
			}
			p, err := zeroparse.Compile(starttest.MarkAll(ast), "main")
			if err != nil {
				t.Fatal(err)
			}
			g, err := sample.New(p, sample.WithSeed(1), sample.WithCoverage())
			if err != nil {
				t.Fatal(err)
			}
			inputs, err := g.Generate(10)
			if errors.Is(err, sample.ErrNoInput) {
				// Some grammars accept no input (their actions always fail at run time).
				for _, in := range c.Inputs {
					if _, err := p.Parse(in); err == nil {
						t.Fatalf("no input generated, but %q parses", in)
					}
				}
				t.Log("no input parses")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, in := range inputs {
				for _, b := range []zeroparse.Backend{zeroparse.Closure, zeroparse.Bytecode, zeroparse.BytecodeIterative} {
					if _, err := p.Parse(in, zeroparse.WithBackend(b)); err != nil {
						t.Errorf("backend %v: generated input %q does not parse: %v", b, in, err)
					}
				}
			}
			t.Logf("%d inputs, %s", len(inputs), g.Coverage())
		})
	}
}
