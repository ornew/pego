package gogen_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/compiletest"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/gentest"
)

// The tests of this package compile the grammars that they generate parsers for with the engine,
// as package pego does.
func init() {
	gentest.Compile = compileForGenerator
}

func parseTest(src string) (*grammar.Grammar, error) { return compiletest.Parse(src) }

// compileForGenerator compiles g for the generators, as the root package's compileForGenerator
// does.
func compileForGenerator(g *grammar.Grammar, opts compile.Options) (*compile.Program, func() (*compile.Program, error), error) {
	prog, err := engine.Compile(g, opts)
	if err != nil {
		return nil, nil, err
	}
	recognizer := func() (*compile.Program, error) {
		rec, err := prog.Recognizer()
		if err != nil {
			return nil, err
		}
		return rec.Static(), nil
	}
	return prog.Static(), recognizer, nil
}

func TestMain(m *testing.M) {
	// Tests build generated programs in fresh temporary directories. Without -trimpath their
	// build cache entries depend on the directory and are never reused, so every run would add
	// tens of gigabytes of builds to the cache. Commands that set GOFLAGS add it themselves.
	// -mod=mod lets the temporary modules that replace this module resolve it without a go.sum.
	os.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -trimpath -mod=mod"))
	code := m.Run()
	if names := gentest.UnwrappedFixtureTests(); len(names) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: tests build generated fixtures without choosing their form: %s\nWrap the test body in gentest.EachMode, or call gentest.NeedsWorkControls, or pin the form with gentest.PinOrdinary\n", strings.Join(names, ", "))
		code = 1
	}
	os.Exit(code)
}
