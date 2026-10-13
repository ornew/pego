package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// PEGO_TYPED_FRAMED_CUT_DIR preserves current direct/reference modules for
// separate-binary paired benchmarks without any generated runtime selector.
func TestGeneratedTypedFramedCutWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_FRAMED_CUT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	control := GenOptions{disableTypedFramedCuts: true}
	t.Run("LR", func(t *testing.T) {
		testGeneratedTypedBodyWorkload(t, `type Doc struct { Items []Match; N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = previous:expr ";" -- items:(x:[a-z] #atomic -- -",") #repeat(3) / items:(x:[a-z] #atomic -- -",") #repeat(3) => Doc{Items: map($items, (e) => $e.x), N: len($items)}
`, typedLRActionWorkloadFixture, filepath.Join(dir, "lr"), "(typed LR body inlined)", control)
	})
	t.Run("OperatorsRule", func(t *testing.T) {
		testGeneratedTypedBodyWorkload(t, `type Atom terminal
type Post struct { X Expr; Items []Match; N int }
type Expr = Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    skip (" " --)*
    operand a:atom -- => $a
    level {
        postfix "[" -- items:(x:[a-z] #atomic -- {len($x) == 1} -",") #repeat(3) "]" => Post{X: $lhs, Items: map($items, (e) => $e.x), N: len($items)}
    }
}
rule atom Atom = "a"
`, typedOperatorsRuleActionWorkloadFixture, filepath.Join(dir, "operatorsRule"), "(typed OperatorsRule body inlined)", control)
	})
}
