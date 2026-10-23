package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		testGeneratedTypedBodyWorkload(t, typedFrameLayoutLRWorkload, fixture, filepath.Join(dir, "lr"), "(typed LR body inlined)", GenOptions{})
	})
	t.Run("OperatorsRule", func(t *testing.T) {
		fixture := strings.ReplaceAll(typedOperatorsRuleActionWorkloadFixture, "post.N != 3", "post.Missing != nil || post.N != 3")
		testGeneratedTypedBodyWorkload(t, typedFrameLayoutOperatorsRuleWorkload, fixture, filepath.Join(dir, "OperatorsRule"), "(typed OperatorsRule body inlined)", GenOptions{}, 2)
	})
}

const typedFrameLayoutLRWorkload = `type Doc = { Items []node[string]; N int; Missing *node[string] }
rule main Doc = e:expr $$ => $e
rule expr Doc = _|_ ignored:"z" #atomic / previous:expr ";" items:(x:[a-z] #atomic -",") #repeat(3) / items:(x:[a-z] #atomic -",") #repeat(3) => Doc{Items: map($items, (e) => e.$x), N: len($items), Missing: $ignored}
`

const typedFrameLayoutOperatorsRuleWorkload = `type Atom = { Text string }
type Post = { X Expr; Items []node[string]; N int; Missing *node[string] }
type Expr ~ Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    skip " "*
    operand atom
    level {
        postfix _|_ ignored:"z" #atomic / "[" items:(x:[a-z] #atomic {len($x) == 1} -",") #repeat(3) "]" => Post{X: $lhs, Items: map($items, (e) => e.$x), N: len($items), Missing: $ignored}
    }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
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
	opts := GenOptions{Package: "layoutfixture", Start: "main", Types: true, Recognize: true}
	for _, c := range []struct{ name, src string }{
		{"lr", typedFrameLayoutLRWorkload}, {"OperatorsRule", typedFrameLayoutOperatorsRuleWorkload},
		{"calculator", string(calculator)}, {"typescript", string(typescript)},
	} {
		b.Run(c.name, func(b *testing.B) {
			g, err := parseTest(c.src)
			if err != nil {
				b.Fatal(err)
			}
			code, err := generateCode(g, opts)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := generateCode(g, opts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(code)), "source-bytes")
		})
	}
}
