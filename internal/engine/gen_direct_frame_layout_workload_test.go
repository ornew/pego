package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

// PEGO_TYPED_FRAME_LAYOUT_DIR preserves same-generator layout controls for
// timing prebuilt fixture binaries independently of generation and compilation.
func TestGeneratedTypedFrameLayoutWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_FRAME_LAYOUT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	t.Run("lr", func(t *testing.T) {
		fixture := strings.ReplaceAll(typedLRActionWorkloadFixture, "saved.N != 3", "saved.Missing != nil || saved.N != 3")
		testGeneratedTypedBodyWorkload(t, typedFrameLayoutLRWorkload, fixture, filepath.Join(dir, "lr"), "(typed LR body inlined)", GenOptions{disableTypedFrameLayouts: true})
	})
	t.Run("pratt", func(t *testing.T) {
		fixture := strings.ReplaceAll(typedPrattActionWorkloadFixture, "post.N != 3", "post.Missing != nil || post.N != 3")
		testGeneratedTypedBodyWorkload(t, typedFrameLayoutPrattWorkload, fixture, filepath.Join(dir, "pratt"), "(typed Pratt body inlined)", GenOptions{disableTypedFrameLayouts: true}, 2)
	})
}

const typedFrameLayoutLRWorkload = `
type Doc struct { Items []Match, N int, Missing *Match }
def main: Doc = e:expr $$ -> $e
def expr: Doc = ((_|_ ignored:@"z") / previous:expr ";" items:(x:@(?a-z) -","){3}
              / items:(x:@(?a-z) -","){3}) -> new Doc{Items: map($items, (e)=>$e.x), N:len($items), Missing:$ignored}
`

const typedFrameLayoutPrattWorkload = `
type Atom terminal
type Post struct { X Expr, Items []Match, N int, Missing *Match }
type Expr = Atom | Post
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    skip " "*
    operand atom
    level { postfix (_|_ ignored:@"z") / "[" items:(x:@(?a-z) [len($x)==1] -","){3} "]"
        -> new Post{X:$lhs, Items:map($items,(e)=>$e.x), N:len($items), Missing:$ignored} }
}
def atom: Atom = "a"
`

// This measures generation separately from matching, using the same source
// and a generation-only environment-selected reference before timing starts.
func BenchmarkTypedFrameLayoutGeneration(b *testing.B) {
	calculator, err := os.ReadFile("../../examples/calculator/calc.pego")
	if err != nil {
		b.Fatal(err)
	}
	typescript, err := os.ReadFile("../../parsers/typescript/typescript.pego")
	if err != nil {
		b.Fatal(err)
	}
	opts := GenOptions{Package: "layoutfixture", Start: "main", Types: true, Recognize: true,
		disableTypedFrameLayouts: os.Getenv("PEGO_REFERENCE_FRAME_LAYOUTS") == "1"}
	for _, c := range []struct{ name, src string }{
		{"lr", typedFrameLayoutLRWorkload}, {"pratt", typedFrameLayoutPrattWorkload},
		{"calculator", string(calculator)}, {"typescript", string(typescript)},
	} {
		b.Run(c.name, func(b *testing.B) {
			g, err := syntax.Parse(c.src)
			if err != nil {
				b.Fatal(err)
			}
			code, err := Generate(g, opts)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := Generate(g, opts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(code)), "source-bytes")
		})
	}
}
