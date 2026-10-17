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
	t.Run("operatorsRule", func(t *testing.T) {
		fixture := strings.ReplaceAll(typedOperatorsRuleActionWorkloadFixture, "post.N != 3", "post.Missing != nil || post.N != 3")
		testGeneratedTypedBodyWorkload(t, typedFrameLayoutOperatorsRuleWorkload, fixture, filepath.Join(dir, "operatorsRule"), "(typed OperatorsRule body inlined)", GenOptions{disableTypedFrameLayouts: true}, 2)
	})
}

const typedFrameLayoutLRWorkload = `type Doc = { Items []Match; N int; Missing *Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = _|_ ignored:"z" #atomic / previous:expr ";" items:(x:[a-z] #atomic -",") #repeat(3) / items:(x:[a-z] #atomic -",") #repeat(3) => Doc{Items: map($items, (e) => e.x), N: len($items), Missing: $ignored}
`

const typedFrameLayoutOperatorsRuleWorkload = `type Atom terminal
type Post = { X Expr; Items []Match; N int; Missing *Match }
type Expr ~ Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    skip " "*
    operand atom
    level {
        postfix _|_ ignored:"z" #atomic / "[" items:(x:[a-z] #atomic {len($x) == 1} -",") #repeat(3) "]" => Post{X: $lhs, Items: map($items, (e) => e.x), N: len($items), Missing: $ignored}
    }
}
rule atom Atom = "a"
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
		{"lr", typedFrameLayoutLRWorkload}, {"operatorsRule", typedFrameLayoutOperatorsRuleWorkload},
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
