package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// PEGO_TYPED_FRAMED_CUT_DIR preserves current direct/reference modules for
// separate-binary paired benchmarks without any generated runtime selector.
func TestGeneratedTypedFramedCutWorkloads(t *testing.T) {
	eachMode(t, testGeneratedTypedFramedCutWorkloads)
}

func testGeneratedTypedFramedCutWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_FRAMED_CUT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	control := GenOptions{}
	t.Run("LR", func(t *testing.T) {
		testGeneratedTypedBodyWorkload(t, `type Doc = { Items []node[string]; N int }
rule main Doc = e:expr $$ => $e
rule expr Doc = previous:expr ";" #cut items:(x:[a-z] #atomic #cut -",") #repeat(3) / items:(x:[a-z] #atomic #cut -",") #repeat(3) => Doc{Items: map($items, (e) => e.$x), N: len($items)}
`, typedLRActionWorkloadFixture, filepath.Join(dir, "lr"), "(typed LR body inlined)", control)
	})
	t.Run("OperatorsRule", func(t *testing.T) {
		testGeneratedTypedBodyWorkload(t, `type Atom = { Text string }
type Post = { X Expr; Items []node[string]; N int }
type Expr ~ Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = #operators {
    skip (" " #cut)*
    operand a:atom #cut => $a
    level {
        postfix "[" #cut items:(x:[a-z] #atomic #cut {len($x) == 1} -",") #repeat(3) "]" => Post{X: $lhs, Items: map($items, (e) => e.$x), N: len($items)}
    }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
`, typedOperatorsRuleActionWorkloadFixture, filepath.Join(dir, "OperatorsRule"), "(typed OperatorsRule body inlined)", control)
	})
}
