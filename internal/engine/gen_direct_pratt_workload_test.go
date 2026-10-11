package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PEGO_TYPED_PRATT_DIR preserves latest-generator controls for paired timing.
func TestGeneratedTypedPrattWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	src, err := os.ReadFile("../../examples/calculator/calc.pego")
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PEGO_TYPED_PRATT_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	fixture := strings.ReplaceAll(typedLRWorkloadFixture, "TestLRWorkload", "TestPrattWorkload")
	fixture = strings.ReplaceAll(fixture, "BenchmarkTypedLR", "BenchmarkTypedPratt")
	testGeneratedTypedBodyWorkload(t, string(src), fixture, dir, "(typed Pratt body inlined)", GenOptions{disableTypedPrattBodies: true})
}

func TestGeneratedTypedPrattActionWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_PRATT_DIR")
	if dir == "" {
		dir = t.TempDir()
	} else {
		dir = filepath.Join(dir, "actions")
	}
	testGeneratedTypedBodyWorkload(t, `
type Atom terminal
type Post struct { X Expr, Items []Match, N int }
type Expr = Atom | Post
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    skip " "*
    operand atom
    level { postfix "[" items:(x:@(?a-z) [len($x) == 1] -","){3} "]"
                -> new Post{X: $lhs, Items: map($items, (e) => $e.x), N: len($items)} }
}
def atom: Atom = "a"
`, typedPrattActionWorkloadFixture, dir, "(typed Pratt body inlined)", GenOptions{disableTypedPrattBodies: true})
}

const typedPrattActionWorkloadFixture = `package lrfixture

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func prattInput(n int) string { return "a" + strings.Repeat(" [b,c,d,]", n) }

func TestPrattWorkload(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		saved, err := ParseAST(prattInput(2), WithUnit(unit))
		post, ok := saved.(*Post)
		if err != nil || !ok || post.N != 3 || len(post.Items) != 3 || post.Items[0].Text != "b" {
			t.Fatalf("typed capture result: %v %v", saved, err)
		}
		before, _ := json.Marshal(saved)
		p := &tparser{parser: &parser{}}
		for _, input := range []string{prattInput(1), prattInput(128), "a", "a[b,c,]", "a[b,c,d,", "a[b,c,d,e,]", "a[b,é,d,]", "", "a[b,c,d,]?"} {
			v, err := p.run(trules[0], input, parseOptions{unit: unit, maxDepth: defaultMaxDepth}, &tslabs{})
			data, _ := json.Marshal(struct { Value any; Error string }{v, fmt.Sprint(err)})
			pooled, pooledErr := ParseAST(input, WithUnit(unit))
			pooledData, _ := json.Marshal(struct { Value any; Error string }{pooled, fmt.Sprint(pooledErr)})
			if string(data) != string(pooledData) { t.Fatal("pooled and reused Pratt parsers differ") }
			fmt.Printf("OBS %v %q %s\n", unit, input, data)
			if (err == nil) != (input == prattInput(1) || input == prattInput(128) || input == "a") {
				t.Fatalf("acceptance %q: %v", input, err)
			}
			if p.depth != 0 || len(p.trail) != 0 || p.frame != nil {
				t.Fatal("Pratt invocation left live frame, depth or undo state")
			}
			for _, u := range p.trail[:cap(p.trail)] {
				if u.f != nil || u.old != nil || u.slot != 0 { t.Fatal("discarded undo retained") }
			}
			p.recycle()
			for _, v := range p.saved[:cap(p.saved)] {
				if v != nil { t.Fatal("Pratt saved captures retained after recycle") }
			}
		}
		after, _ := json.Marshal(saved)
		if string(before) != string(after) { t.Fatal("returned Pratt AST changed") }
	}
}

var prattResult Expr

func BenchmarkTypedPratt(b *testing.B) {
	for _, n := range []int{1, 128} {
		input := prattInput(n)
		for _, unit := range []Unit{CodePoints, Bytes} {
			b.Run(fmt.Sprintf("%d/%v", n, unit), func(b *testing.B) {
				if _, err := ParseAST(input, WithUnit(unit)); err != nil { b.Fatal(err) }
				b.ReportAllocs()
				for b.Loop() {
					v, err := ParseAST(input, WithUnit(unit))
					if err != nil { b.Fatal(err) }
					prattResult = v
				}
			})
		}
	}
}
`
