package complexitytest

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/bench/inputs"
)

// corpusCap bounds the budget of one corpus parse; a linear grammar stays far below it.
const corpusCap = 2_000_000_000

// ratioLimit is the largest growth of the worst cost per input byte from the input of size 2n to
// the input of size 4n for a linear grammar. Linear growth keeps the ratio near 1, and a fixed
// cost lowers it. Without fixed cost the limit admits growth up to n^1.14; the fixed cost that
// remains at 2n can lower the ratio of a linear grammar to about 0.97, and then the limit
// admits up to about n^1.18. The ratio starts at 2n because the cost per byte of the smallest
// input still carries the fixed cost.
const ratioLimit = 1.1

// corpusEntry is a grammar of the repository with a family of inputs that grow with n.
type corpusEntry struct {
	name    string
	grammar string // path of the grammar file from this package
	// build returns variant v of the input of size n. Every variant parses.
	build    func(n, v int) string
	variants int
	// sizes are n of the series n, 2n, 4n; shortSizes are used with testing.Short.
	sizes, shortSizes []int
	// order is the measured growth order of the worst variant. A grammar whose order is not
	// Linear is a defect of the grammar or of the engine and is listed in the test report.
	order Order
	// source says where the inputs come from.
	source string
	// syntaxErrors marks inputs that the grammar reports syntax errors for, such as inputs with
	// recovered errors; the parse finishes with a *pego.SyntaxError or pego.SyntaxErrors.
	syntaxErrors bool
}

// repeated builds an input from a header and n units. A unit has a %d verb for its index.
func repeated(header string, units []string, sep string) func(n, v int) string {
	return func(n, v int) string {
		var b strings.Builder
		b.WriteString(header)
		for i := range n {
			if i > 0 {
				b.WriteString(sep)
			}
			fmt.Fprintf(&b, units[v], i)
		}
		return b.String()
	}
}

// corpusEntries lists the grammars. The bench grammars use the benchmark inputs. The large
// grammars of parsers/ have no scalable inputs in the repository, and the inputs of package
// sample stay short whatever MaxLen is (the longest is below 60 bytes), so a few hand-written
// units are repeated instead.
func corpusEntries() []corpusEntry {
	const bench = "benchmark inputs"
	const units = "hand-written units repeated under a list-like start rule"
	one := func(f func(n int) string) func(n, v int) string { return func(n, _ int) string { return f(n) } }
	return []corpusEntry{
		{name: "json", grammar: "../../parsers/json/json.pego", build: one(inputs.JSONInput), variants: 1,
			sizes: double(4096), shortSizes: double(1024), order: Linear, source: bench},
		{name: "csv", grammar: "../../parsers/csv/csv.pego", build: one(inputs.CSVInput), variants: 1,
			sizes: double(100), shortSizes: double(25), order: Linear, source: bench},
		{name: "xml", grammar: "../../parsers/xml/xml.pego", build: one(inputs.XMLInput), variants: 1,
			sizes: double(4096), shortSizes: double(1024), order: Linear, source: bench},
		{name: "calc operators rule", grammar: "../../examples/calculator/calc.pego", build: one(inputs.ArithInput), variants: 1,
			sizes: double(100), shortSizes: double(25), order: Linear, source: bench},
		{name: "calc left recursion", grammar: "../../examples/calculator/calc_lr.pego", build: one(inputs.ArithInput), variants: 1,
			sizes: double(100), shortSizes: double(25), order: Linear, source: bench},
		{name: "minilang", grammar: "../../examples/minilang/minilang.pego",
			build: one(func(n int) string { return inputs.MinilangInput(n, 0) }), variants: 1,
			sizes: double(10), shortSizes: double(5), order: Linear, source: bench},
		{name: "minilang recovery", grammar: "../../examples/minilang/minilang.pego",
			build: one(func(n int) string { return inputs.MinilangInput(n, 7) }), variants: 1, syntaxErrors: true,
			sizes: double(10), shortSizes: double(5), order: Linear, source: bench},
		{name: "outline", grammar: "../../examples/outline/outline.pego", build: one(inputs.OutlineInput), variants: 1,
			sizes: double(100), shortSizes: double(25), order: Linear, source: bench},
		{name: "golang", grammar: "../../parsers/golang/golang.pego", variants: 3, source: units,
			build: repeated("package p\n\n", []string{
				"func f%d(a, b int) int {\n\treturn a*b + 1\n}\n\n",
				"type T%d struct {\n\tA int\n\tB string\n}\n\n",
				"var v%d = map[string][]int{\"a\": {1, 2}, \"b\": {3}}\n",
			}, ""),
			sizes: double(40), shortSizes: double(10), order: Linear},
		{name: "python", grammar: "../../parsers/python/python.pego", variants: 3, source: units,
			build: repeated("", []string{
				"def f%d(x):\n    return x + 1\n\n",
				"class C%d:\n    def m(self, a, b=1):\n        if a:\n            return b\n        return None\n\n",
				"x%d = [i * 2 for i in range(10)]\n",
			}, ""),
			sizes: double(40), shortSizes: double(10), order: Linear},
		{name: "cel", grammar: "../../parsers/cel/cel.pego", variants: 3, source: units,
			build: repeated("true", []string{
				" && request.auth.claims.email.endsWith(\"@example.com\") && %d > 0",
				" && (resource.name.startsWith(\"p-\") || \"admin\" in groups) && %d > 0",
				" && items.exists(k, k == %d)",
			}, ""),
			sizes: double(40), shortSizes: double(10), order: Linear},
		{name: "cue", grammar: "../../parsers/cue/cue.pego", variants: 3, source: units,
			build: repeated("package p\n\n", []string{
				"a%d: 1 + 2\n",
				"#D%d: {name!: string, age?: int & >=0}\n",
				"l%d: [for i in [1, 2, 3] if i > 1 {i * 2}]\n",
			}, ""),
			sizes: double(40), shortSizes: double(10), order: Linear},
		{name: "duckdb", grammar: "../../parsers/duckdb/duckdb.pego", variants: 3, source: units,
			build: repeated("", []string{
				"SELECT a, b FROM t WHERE a > %d;\n",
				"INSERT INTO t VALUES (%d, 'x');\n",
				"WITH r AS (SELECT o.id, sum(o.total) AS s FROM orders AS o GROUP BY o.id) SELECT id, s FROM r WHERE id > %d ORDER BY s DESC LIMIT 10;\n",
			}, ""),
			sizes: double(40), shortSizes: double(10), order: Linear},
		{name: "typescript", grammar: "../../parsers/typescript/typescript.pego", variants: 3, source: units,
			build: repeated("", []string{
				"export function f%d(a: number, b?: string): number {\n  return a * 2 + 1;\n}\n",
				"interface P%d<T = number> {\n  x: T;\n  readonly y?: T;\n}\n",
				"const v%d = async <T,>(xs: T[]) => xs.map((x) => x as unknown as string);\n",
			}, ""),
			sizes: double(40), shortSizes: double(10), order: Linear},
		{name: "yaml", grammar: "../../parsers/yaml/yaml.pego", variants: 3, source: units,
			build: repeated("", []string{
				"key%d: value\n",
				"- item%d\n",
				"k%d:\n  - a\n  - {b: 1, c: [2, 3]}\n",
			}, ""),
			sizes: double(40), shortSizes: double(10), order: Linear},
	}
}

// TestCorpus measures the grammars of the repository on realistic inputs. For every grammar and
// backend it checks that
//   - every input parses without exhausting the cap;
//   - for a Linear grammar, the worst budget per input byte at 4n is at most ratioLimit times the
//     worst at 2n; otherwise the series classifies as the recorded order.
func TestCorpus(t *testing.T) {
	t.Parallel()
	for _, e := range corpusEntries() {
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(e.grammar)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Compile(string(src))
			if err != nil {
				t.Fatal(err)
			}
			sizes := e.sizes
			if testing.Short() {
				sizes = e.shortSizes
			}
			for _, b := range Backends {
				// worst[i] is the largest budget per byte of size i over the variants; series
				// holds the worst variant by cost at every size.
				worst := make([]float64, len(sizes))
				series := Series{Sizes: sizes, Costs: make([]Cost, len(sizes))}
				for i, n := range sizes {
					for v := range e.variants {
						in := e.build(n, v)
						measure := Steps
						if e.syntaxErrors {
							measure = StepsLenient
						}
						c, err := measure(p, in, b, corpusCap)
						if err != nil {
							t.Fatalf("%s: size %d variant %d (%d bytes): %v", BackendName(b), n, v, len(in), err)
						}
						if e.syntaxErrors && c.ParseErr == nil {
							t.Fatalf("%s: size %d variant %d reports no syntax error", BackendName(b), n, v)
						}
						if c.Capped {
							t.Fatalf("%s: size %d variant %d reached the cap", BackendName(b), n, v)
						}
						worst[i] = max(worst[i], float64(c.Steps)/float64(len(in)))
						if c.Steps > series.Costs[i].Steps {
							series.Costs[i] = c
						}
					}
				}
				ratio := worst[len(worst)-1] / worst[len(worst)-2]
				t.Logf("%s %s: worst steps per byte %.1f, ratio 4n/2n %.3f, slopes %.2f, order %v", e.name, BackendName(b), worst, ratio, series.Slopes(), Classify(series))
				if e.order == Linear {
					if ratio > ratioLimit {
						t.Errorf("%s: steps per byte grow by %.3f from 2n to 4n (limit %.1f)", BackendName(b), ratio, ratioLimit)
					}
				} else if got := Classify(series); got != e.order {
					t.Errorf("%s: classified %v, want %v", BackendName(b), got, e.order)
				}
			}
		})
	}
}
