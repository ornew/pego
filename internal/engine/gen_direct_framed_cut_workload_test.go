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
		testGeneratedTypedBodyWorkload(t, `
type Doc struct { Items []Match, N int }
def main: Doc = e:expr $$ -> $e
def expr: Doc = (previous:expr ";" -- items:(x:@(?a-z) -- -","){3}
              / items:(x:@(?a-z) -- -","){3}) -> new Doc{Items: map($items, (e) => $e.x), N: len($items)}
`, typedLRActionWorkloadFixture, filepath.Join(dir, "lr"), "(typed LR body inlined)", control)
	})
	t.Run("Pratt", func(t *testing.T) {
		testGeneratedTypedBodyWorkload(t, `
type Atom terminal
type Post struct { X Expr, Items []Match, N int }
type Expr = Atom | Post
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    skip (" " --)*
    operand a:atom -- -> $a
    level { postfix "[" -- items:(x:@(?a-z) -- [len($x) == 1] -","){3} "]"
                -> new Post{X: $lhs, Items: map($items, (e) => $e.x), N: len($items)} }
}
def atom: Atom = "a"
`, typedPrattActionWorkloadFixture, filepath.Join(dir, "pratt"), "(typed Pratt body inlined)", control)
	})
}
