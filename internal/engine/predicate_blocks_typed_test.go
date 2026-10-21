package engine

import (
	"path/filepath"
	"runtime"
	"testing"
)

// These declared typed-result fixtures force typed runtime execution; ordinary
// Node parser parity alone does not cover generated typed Predicate Actions.
func TestPredicateBlocksGeneratedTypedParity(t *testing.T) {
	cases := []genCase{
		{"typed bindings", `type Word = { Text string }
type Row = { Word Word; Code int }
var n int
rule main Row = {n := 1; n == 1} w:word $$ => Row{Word: $w, Code: n}
rule word Word = t:("é") #atomic => Word{Text: text($t)}`, []string{"é", "", "a", "éé"}},
		{"typed choice rollback", `type Word = { Text string }
type Row = { Word Word; Code int }
var n int
rule main Row = {n := 0} w:word ({n := 1; false} / {n := 2; n == 2}) $$ => Row{Word: $w, Code: n}
rule word Word = t:("é") #atomic => Word{Text: text($t)}`, []string{"é", "", "a", "éé"}},
	}
	testGeneratedTypesCorpus(t, filepath.Join(runtime.GOROOT(), "bin", "go"), cases, GenOptions{convertTypes: true}, len(cases))
}
