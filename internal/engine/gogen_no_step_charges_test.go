package engine

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// Without work controls no function of the parser package or the types package charges steps, including the
// typed constructors, the conversions of typed values and the closures of actions, which the specialization of
// the rule functions does not reach.
func TestGeneratedNoStepChargesInTypedCode(t *testing.T) {
	t.Parallel()
	for _, src := range []string{ordinaryOnlyTyped, gentest.OrdinaryOnlyConverted} {
		g, err := parseTest(src)
		if err != nil {
			t.Fatal(err)
		}
		// generated returns the source of the parser package and of the types package.
		generated := func(opts gen.GenOptions) string {
			t.Helper()
			opts.Package, opts.Module, opts.Start, opts.Runtime = "p", "example.com/p", "main", gen.RuntimeImport
			files, err := gentest.Generate(t, g, opts)
			if err != nil {
				t.Fatal(err)
			}
			all := ""
			for _, f := range files {
				if f.Path == "parser.go" || strings.HasSuffix(f.Path, "types.go") {
					all += string(f.Content)
				}
			}
			if all == "" {
				t.Fatal("no parser.go")
			}
			return all
		}
		for _, opts := range []gen.GenOptions{{Types: true}, {Types: true, TypedOnly: true}, {Types: true, Document: true}, {Types: true, Recognize: true, Document: true}} {
			if out := generated(opts); strings.Contains(out, "StepWork") {
				t.Errorf("%+v: a parser without work controls charges steps", opts)
			}
		}
		if out := generated(gen.GenOptions{Types: true, Document: true, WorkControls: true}); !strings.Contains(out, "StepWork") {
			t.Error("a controlled parser does not charge steps: the check is void")
		}
	}
	// The conversions of the types package charge steps only in a controlled parser, for a list.
	g, err := parseTest(gentest.OrdinaryOnlyConverted)
	if err != nil {
		t.Fatal(err)
	}
	files, err := gentest.Generate(t, g, gen.GenOptions{Package: "p", Module: "example.com/p", Start: "main", Runtime: gen.RuntimeImport, Types: true, WorkControls: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if strings.HasSuffix(f.Path, "types.go") && strings.Contains(string(f.Content), "StepWork") {
			found = true
		}
	}
	if !found {
		t.Error("the types package of a controlled parser does not charge steps: the check of the types package is void")
	}
}
