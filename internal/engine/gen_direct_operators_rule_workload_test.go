package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PEGO_TYPED_OPERATORS_RULE_DIR preserves latest-generator controls for paired timing.
func TestGeneratedTypedOperatorsRuleWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	src, err := os.ReadFile("../../examples/calculator/calc.pego")
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PEGO_TYPED_OPERATORS_RULE_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	fixture := strings.ReplaceAll(typedLRWorkloadFixture, "TestLRWorkload", "TestOperatorsRuleWorkload")
	fixture = strings.ReplaceAll(fixture, "BenchmarkTypedLR", "BenchmarkTypedOperatorsRule")
	testGeneratedTypedBodyWorkload(t, string(src), fixture, dir, "(typed OperatorsRule body inlined)", GenOptions{disableTypedOperatorsRuleBodies: true})
}

func TestGeneratedTypedOperatorsRuleActionWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	dir := os.Getenv("PEGO_TYPED_OPERATORS_RULE_DIR")
	if dir == "" {
		dir = t.TempDir()
	} else {
		dir = filepath.Join(dir, "actions")
	}
	testGeneratedTypedBodyWorkload(t, `type Atom = { Text string }
type Post = { X Expr; Items []node[string]; N int }
type Expr ~ Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    skip " "*
    operand atom
    level {
        postfix "[" items:(x:[a-z] #atomic {len($x) == 1} -",") #repeat(3) "]" => Post{X: $lhs, Items: map($items, (e) => e.$x), N: len($items)}
    }
}
rule atom Atom = t:("a") #atomic => Atom{Text: text($t)}
`, typedOperatorsRuleActionWorkloadFixture, dir, "(typed OperatorsRule body inlined)", GenOptions{disableTypedOperatorsRuleBodies: true})
}

const typedOperatorsRuleActionWorkloadFixture = `package lrfixture

import (
	pegort "github.com/ornew/pego/runtime"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func operatorsRuleInput(n int) string { return "a" + strings.Repeat(" [b,c,d,]", n) }

func TestOperatorsRuleWorkload(t *testing.T) {
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		saved, err := ParseAST(operatorsRuleInput(2), WithUnit(unit))
		post, ok := saved.(*Post)
		if err != nil || !ok || post.N != 3 || len(post.Items) != 3 || post.Items[0].Value != "b" {
			t.Fatalf("typed capture result: %v %v", saved, err)
		}
		before, _ := json.Marshal(saved)
		p := &pegort.TParser{Parser: &pegort.Parser{}}
		for _, input := range []string{operatorsRuleInput(1), operatorsRuleInput(128), "a", "a[b,c,]", "a[b,c,d,", "a[b,c,d,e,]", "a[b,é,d,]", "", "a[b,c,d,]?"} {
			v, err := p.X_run(trules[0], input, pegort.X_parseOptions{X_unit: unit, X_maxDepth: pegoGrammar.X_maxDepth}, &tslabs{})
			data, _ := json.Marshal(struct { Value any; Error string }{v, fmt.Sprint(err)})
			pooled, pooledErr := ParseAST(input, WithUnit(unit))
			pooledData, _ := json.Marshal(struct { Value any; Error string }{pooled, fmt.Sprint(pooledErr)})
			if string(data) != string(pooledData) { t.Fatal("pooled and reused OperatorsRule parsers differ") }
			fmt.Printf("OBS %v %q %s\n", unit, input, data)
			if (err == nil) != (input == operatorsRuleInput(1) || input == operatorsRuleInput(128) || input == "a") {
				t.Fatalf("acceptance %q: %v", input, err)
			}
			if p.Depth != 0 || len(p.Trail) != 0 || p.Frame != nil {
				t.Fatal("OperatorsRule invocation left live frame, depth or undo state")
			}
			for _, u := range p.Trail[:cap(p.Trail)] {
				if u.X_f != nil || u.X_old != nil || u.X_slot != 0 { t.Fatal("discarded undo retained") }
			}
			p.X_recycle()
			for _, v := range p.X_saved[:cap(p.X_saved)] {
				if v != nil { t.Fatal("OperatorsRule saved captures retained after recycle") }
			}
		}
		after, _ := json.Marshal(saved)
		if string(before) != string(after) { t.Fatal("returned OperatorsRule AST changed") }
	}
}

var operatorsRuleResult Expr

func BenchmarkTypedOperatorsRule(b *testing.B) {
	for _, n := range []int{1, 128} {
		input := operatorsRuleInput(n)
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			b.Run(fmt.Sprintf("%d/%v", n, unit), func(b *testing.B) {
				if _, err := ParseAST(input, WithUnit(unit)); err != nil { b.Fatal(err) }
				b.ReportAllocs()
				for b.Loop() {
					v, err := ParseAST(input, WithUnit(unit))
					if err != nil { b.Fatal(err) }
					operatorsRuleResult = v
				}
			})
		}
	}
}
`
