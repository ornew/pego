package starttest_test

import (
	"testing"

	"github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/starttest"
	"github.com/ornew/zeroparse/internal/syntax"
)

func TestMarkAllSkipsHiddenRules(t *testing.T) {
	g, err := syntax.Parse("rule _a = \"x\"\nrule b = _a\nrule c = b\n")
	if err != nil {
		t.Fatal(err)
	}
	prog, err := engine.Compile(starttest.MarkAll(g), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := prog.Starts(); len(got) != 2 || got[0] != "b" || got[1] != "c" || prog.DefaultStart() != "b" {
		t.Errorf("starts %v default %q", got, prog.DefaultStart())
	}
}

// A grammar that already has declaration attributes keeps them.
func TestMarkAllKeepsAttributes(t *testing.T) {
	g, err := syntax.Parse("rule a = \"x\"\n@start\nrule b = a\n")
	if err != nil {
		t.Fatal(err)
	}
	prog, err := engine.Compile(starttest.MarkAll(g), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := prog.Starts(); len(got) != 1 || got[0] != "b" {
		t.Errorf("starts %v", got)
	}
}
