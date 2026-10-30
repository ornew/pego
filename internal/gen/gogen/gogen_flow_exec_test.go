package gogen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/gogen"
	"github.com/ornew/pego/internal/gentest"
)

// execGenerate generates the parser of the grammar source with the options, joined into one text,
// with the functions that the Exec IR switch routes printed from Exec IR if exec, and returns the
// number of those functions, or reports false if the generator rejects the grammar.
func execGenerate(src string, opts gen.GenOptions, exec bool) (string, int, bool) {
	g, err := parseTest(src)
	if err != nil {
		return "", 0, false
	}
	var last *gogen.Generator
	h := &gogen.Hooks{ExecRoute: exec, Generator: func(x *gogen.Generator) { last = x }}
	opts.Module, opts.KeepComments = "example.com/p", true
	files, err := gogen.GenerateParser(g, compileForGenerator, opts, h)
	if err != nil {
		return "", 0, false
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "== %s\n", f.Path)
		b.Write(f.Content)
	}
	n := 0
	if last != nil {
		n = last.ExecFuncs
	}
	return b.String(), n, true
}

// TestExecRoutePrintsTheSame checks that every corpus grammar generates the same code with the
// functions that the Exec IR switch routes printed from Exec IR as from Flow IR, in the modes of
// TestFlowPrintsTypedRules that print rules in the ordinary form: without budget controls or a
// Document, whose rules the switch does not route.
func TestExecRoutePrintsTheSame(t *testing.T) {
	t.Parallel()
	cases := append(gentest.CorpusWithParsers(t), gentest.CoreExtraGrammars...)
	for _, mode := range flowModes {
		if mode.opts.BudgetControls || mode.opts.Document {
			continue
		}
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			compared, routed := 0, 0
			for _, gc := range cases {
				want, _, ok := execGenerate(gc.Src, mode.opts, false)
				if !ok {
					continue
				}
				got, n, ok := execGenerate(gc.Src, mode.opts, true)
				if !ok {
					t.Errorf("%s: rejected from Exec IR", gc.Name)
					continue
				}
				compared++
				routed += n
				if got != want {
					t.Errorf("%s: the code from Exec IR differs:\n%s", gc.Name, firstDiff(want, got))
				}
			}
			t.Logf("compared %d grammars; %d functions printed from Exec IR", compared, routed)
			if compared < 100 {
				t.Errorf("compared %d grammars", compared)
			}
			if routed < 1000 {
				t.Errorf("%d functions printed from Exec IR", routed)
			}
		})
	}
}
