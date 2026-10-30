package tsgen_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gen/tsgen"
	"github.com/ornew/pego/internal/gentest"
)

// execGenerate generates the TypeScript parser of the grammar source with the options, joined into
// one text, with the functions printed from Flow IR printed from Exec IR if exec, and returns the
// number of those functions, or reports false if the generator rejects the grammar.
func execGenerate(src string, opts gen.GenOptions, exec bool) (string, int, bool) {
	g, err := parseTest(src)
	if err != nil {
		return "", 0, false
	}
	n := 0
	h := &tsgen.Hooks{ExecRoute: exec, Generator: func(x *tsgen.Generator) { n = x.ExecFuncs }}
	files, err := tsgen.GenerateParser(g, compileForGenerator, opts, h)
	if err != nil {
		return "", 0, false
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "== %s\n", f.Path)
		b.Write(f.Content)
	}
	return b.String(), n, true
}

// TestExecRoutePrintsTheSame checks that every corpus grammar generates the same TypeScript with the
// functions printed from Flow IR printed from Exec IR, in each mode that changes how they print.
func TestExecRoutePrintsTheSame(t *testing.T) {
	t.Parallel()
	cases := append(gentest.CorpusWithParsers(t), gentest.CoreExtraGrammars...)
	modes := []struct {
		name string
		opts gen.GenOptions
	}{
		{"plain", gen.GenOptions{}},
		{"recognize", gen.GenOptions{Recognize: true}},
		{"budget", gen.GenOptions{Recognize: true, BudgetControls: true}},
		{"observe", gen.GenOptions{Observability: true}},
		{"types", gen.GenOptions{Types: true}},
		{"default value", gen.GenOptions{DefaultValue: true}},
	}
	for _, mode := range modes {
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

// firstDiff returns the lines around the first difference of a and b.
func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	i := 0
	for i < len(al) && i < len(bl) && al[i] == bl[i] {
		i++
	}
	var out strings.Builder
	for k := max(0, i-8); k < i+8; k++ {
		x, y := "", ""
		if k < len(al) {
			x = al[k]
		}
		if k < len(bl) {
			y = bl[k]
		}
		mark := " "
		if x != y {
			mark = "!"
		}
		fmt.Fprintf(&out, "%s %-60s | %s\n", mark, x, y)
	}
	return out.String()
}
