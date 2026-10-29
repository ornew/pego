package engine

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ornew/pego/internal/gen"
	"github.com/ornew/pego/internal/gentest"
)

// These declared typed-result fixtures force typed runtime execution; ordinary
// Node parser parity alone does not cover generated typed Predicate Actions.
func TestPredicateBlocksGeneratedTypedParity(t *testing.T) {
	t.Parallel()
	gentest.EachMode(t, testPredicateBlocksGeneratedTypedParity)
}

func testPredicateBlocksGeneratedTypedParity(t *testing.T) {
	cases := []gentest.Case{
		{Name: "typed bindings", Src: `type Word = { Text string }
type Row = { Word Word; Code int }
var n int
rule main Row = {n := 1; n == 1} w:word $$ => Row{Word: $w, Code: n}
rule word Word = t:("é") #atomic => Word{Text: text($t)}`, Inputs: []string{"é", "", "a", "éé"}},
		{Name: "typed choice rollback", Src: `type Word = { Text string }
type Row = { Word Word; Code int }
var n int
rule main Row = {n := 0} w:word ({n := 1; false} / {n := 2; n == 2}) $$ => Row{Word: $w, Code: n}
rule word Word = t:("é") #atomic => Word{Text: text($t)}`, Inputs: []string{"é", "", "a", "éé"}},
	}
	gentest.GeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, gen.GenOptions{ConvertTypes: true}, len(cases))
}
