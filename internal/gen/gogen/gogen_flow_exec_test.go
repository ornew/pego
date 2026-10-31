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
// number of those functions by table, or reports false if the generator rejects the grammar.
func execGenerate(src string, opts gen.GenOptions, exec bool) (string, map[string]int, bool) {
	g, err := parseTest(src)
	if err != nil {
		return "", nil, false
	}
	var last *gogen.Generator
	h := &gogen.Hooks{ExecRoute: exec, Generator: func(x *gogen.Generator) { last = x }}
	opts.Module, opts.KeepComments = "example.com/p", true
	files, err := gogen.GenerateParser(g, compileForGenerator, opts, h)
	if err != nil {
		return "", nil, false
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "== %s\n", f.Path)
		b.Write(f.Content)
	}
	var n map[string]int
	if last != nil {
		n = last.ExecTables
	}
	return b.String(), n, true
}

// execModes are the generator options under which the Exec IR switch routes the rules of the
// tables (want) that print in the ordinary form: the modes of TestFlowPrintsTypedRules without
// budget controls or a Document, and those of the Node runtime alone. A stream mode runs the grammars
// with a #stream repetition (streamCases), too few for counts by table.
var execModes = func() []execMode {
	var modes []execMode
	for _, m := range flowModes {
		if !m.opts.BudgetControls && !m.opts.Document {
			want := []string{"trules"}
			if m.opts.Recognize {
				want = append(want, "recRules")
			}
			modes = append(modes, execMode{m.name, m.opts, want})
		}
	}
	return append(modes,
		execMode{"nodes", gen.GenOptions{}, []string{"rules"}},
		execMode{"recognizer", gen.GenOptions{Recognize: true}, []string{"rules", "recRules"}},
		execMode{"stream", gen.GenOptions{Stream: true}, nil},
		execMode{"typed stream", gen.GenOptions{Types: true, Stream: true}, nil},
		execMode{"source info", gen.GenOptions{SourceInfo: true}, []string{"rules"}},
		execMode{"default value", gen.GenOptions{DefaultValue: true}, []string{"rules"}},
		execMode{"observable nodes", gen.GenOptions{Observability: true}, []string{"rules"}},
	)
}()

type execMode struct {
	name string
	opts gen.GenOptions
	want []string
}

// TestExecRoutePrintsTheSame checks that every corpus grammar generates the same code with the
// functions that the Exec IR switch routes printed from Exec IR as from Flow IR, in the modes that
// print rules in the ordinary form (execModes), and that the switch routes the rules of each table
// that the mode prints.
func TestExecRoutePrintsTheSame(t *testing.T) {
	t.Parallel()
	corpus := append(gentest.CorpusWithParsers(t), gentest.CoreExtraGrammars...)
	streams := streamCases(t)
	for _, mode := range execModes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			cases, least := corpus, 100
			if mode.opts.Stream {
				cases, least = streams, len(streams)*3/4
			}
			compared, routed := 0, map[string]int{}
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
				for table, k := range n {
					routed[table] += k
				}
				if got != want {
					t.Errorf("%s: the code from Exec IR differs:\n%s", gc.Name, firstDiff(want, got))
				}
			}
			t.Logf("compared %d grammars; functions printed from Exec IR by table: %v", compared, routed)
			if compared < least {
				t.Errorf("compared %d grammars", compared)
			}
			for _, table := range mode.want {
				if routed[table] < 1000 {
					t.Errorf("%d functions of table %s printed from Exec IR", routed[table], table)
				}
			}
		})
	}
}
