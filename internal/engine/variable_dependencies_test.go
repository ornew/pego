package engine

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/syntax"
)

// ruleVariablesDFS retains the previous per-root closure as an independent
// scheduling oracle. Missing rules have no direct reads or outgoing calls.
func ruleVariablesDFS(rules []*grammar.RuleDef) map[string][]string {
	direct := map[string]map[string]bool{}
	calls := map[string][]string{}
	for _, r := range rules {
		set := map[string]bool{}
		walkRuleTerms(r, func(t grammar.Term) {
			if v, ok := t.(*grammar.VarRef); ok {
				set[v.Name] = true
			}
		})
		direct[r.Name], calls[r.Name] = set, allCalls(r.Expr, nil)
	}
	out := map[string][]string{}
	for _, r := range rules {
		seen, all := map[string]bool{r.Name: true}, map[string]bool{}
		stack := []string{r.Name}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for v := range direct[n] {
				all[v] = true
			}
			for _, c := range calls[n] {
				if !seen[c] {
					seen[c] = true
					stack = append(stack, c)
				}
			}
		}
		if len(all) > 0 {
			names := make([]string, 0, len(all))
			for v := range all {
				names = append(names, v)
			}
			slices.Sort(names)
			out[r.Name] = names
		}
	}
	return out
}

func variableChain(n int, reverse, reads bool) string {
	var src strings.Builder
	src.WriteString("type V struct { N int }\ndef main = [x=7] r0\n")
	for j := range n {
		i := j
		if reverse {
			i = n - 1 - j
		}
		if i == n-1 {
			action := "7"
			if reads {
				action = "x"
			}
			fmt.Fprintf(&src, "def r%d = \"a\" -> new V{N:%s}\n", i, action)
		} else {
			fmt.Fprintf(&src, "def r%d = r%d\n", i, i+1)
		}
	}
	return src.String()
}

func variableGraph(n int, cyclic bool) string {
	var src strings.Builder
	src.WriteString("type V struct { N int }\ndef main = [z=1] [a=2] [m=3] r0\ndef leaf = \"b\" -> new V{N:z+a+m}\n")
	for i := range n {
		if cyclic {
			fmt.Fprintf(&src, "def r%d:node = \"a\" r%d / leaf\n", i, (i+1)%n)
		} else if i+2 < n {
			fmt.Fprintf(&src, "def r%d = r%d / r%d / r%d\n", i, i+1, i+2, i+1)
		} else {
			fmt.Fprintf(&src, "def r%d = leaf / leaf\n", i)
		}
	}
	return src.String()
}

func variableManyReads(n int, cyclic bool) string {
	var src strings.Builder
	src.WriteString("type V struct { N int }\ndef main = r0\n")
	for i := range n {
		if cyclic {
			fmt.Fprintf(&src, "def r%d:node = \"a\" r%d / \"b\" -> new V{N:v%d}\n", i, (i+1)%n, i)
		} else if i+1 < n {
			fmt.Fprintf(&src, "def r%d:node = r%d / \"b\" -> new V{N:v%d}\n", i, i+1, i)
		} else {
			fmt.Fprintf(&src, "def r%d:node = \"b\" -> new V{N:v%d}\n", i, i)
		}
	}
	return src.String()
}

func TestVariableDependencyGrowingSets(t *testing.T) {
	for _, cyclic := range []bool{false, true} {
		g, err := syntax.Parse(variableManyReads(64, cyclic))
		if err != nil {
			t.Fatal(err)
		}
		want := ruleVariablesDFS(g.Rules())
		if len(want["r0"]) != 64 {
			t.Fatalf("r0 only has %d variables", len(want["r0"]))
		}
		for seed := range int64(10) {
			rules := append([]*grammar.RuleDef(nil), g.Rules()...)
			rand.New(rand.NewSource(seed)).Shuffle(len(rules), func(i, j int) { rules[i], rules[j] = rules[j], rules[i] })
			if got := ruleVariables(rules); !reflect.DeepEqual(got, want) {
				t.Fatalf("cyclic=%v seed=%d: changed growing sets", cyclic, seed)
			}
		}
	}
}

func TestVariableDependencyGraphs(t *testing.T) {
	for _, n := range []int{64, 512, 2048} {
		for _, reverse := range []bool{false, true} {
			for _, reads := range []bool{false, true} {
				t.Run(fmt.Sprintf("chain/%d/reverse=%v/reads=%v", n, reverse, reads), func(t *testing.T) {
					g, err := syntax.Parse(variableChain(n, reverse, reads))
					if err != nil {
						t.Fatal(err)
					}
					got := ruleVariables(g.Rules())
					if !reflect.DeepEqual(got, ruleVariablesDFS(g.Rules())) {
						t.Fatal("changed transitive sets")
					}
					if !reads {
						if got == nil || len(got) != 0 {
							t.Fatalf("no-read result: %v", got)
						}
					} else {
						for _, rd := range g.Rules() {
							if !reflect.DeepEqual(got[rd.Name], []string{"x"}) {
								t.Errorf("%s: %v", rd.Name, got[rd.Name])
							}
						}
					}
				})
			}
		}
	}
	for _, cyclic := range []bool{false, true} {
		g, err := syntax.Parse(variableGraph(128, cyclic))
		if err != nil {
			t.Fatal(err)
		}
		want := ruleVariablesDFS(g.Rules())
		for _, rd := range g.Rules() {
			if !reflect.DeepEqual(want[rd.Name], []string{"a", "m", "z"}) {
				t.Fatalf("oracle %s: %v", rd.Name, want[rd.Name])
			}
		}
		for seed := range int64(30) {
			rules := append([]*grammar.RuleDef(nil), g.Rules()...)
			rand.New(rand.NewSource(seed)).Shuffle(len(rules), func(i, j int) { rules[i], rules[j] = rules[j], rules[i] })
			if got := ruleVariables(rules); !reflect.DeepEqual(got, want) {
				t.Fatalf("cyclic=%v seed=%d: changed sets", cyclic, seed)
			}
		}
	}
	if got := ruleVariables(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty grammar: %v", got)
	}
}

func TestVariableDependencyTraversal(t *testing.T) {
	// Include calls in all syntactic contexts, even those which may not run;
	// recovery arguments and Pratt actions have the same dependency contract.
	src := `def main = (&look !negative -discarded optional? repeated*) / attributed
 def look = [z==1] "a"
 def negative = [a==1] "b"
 def discarded = [m==1] "c"
 def optional = [opt==1] "d"
 def repeated = [rep==1] "e"
 def attributed = "f" #recover(sync=recovery)
 def recovery = [recoverVar==1] "g"
 def pr = pratt { skip skipRule operand operandRule -> new V{N:operandVar} level { prefix prefixRule -> new V{N:prefixVar} infix left infixRule -> new V{N:infixVar} postfix postfixRule -> new V{N:postfixVar} } }
 def skipRule = [skipVar==1] " "
 def operandRule = [atomVar==1] "a"
 def prefixRule = [preVar==1] "-"
 def infixRule = [inVar==1] "+"
 def postfixRule = [postVar==1] "!"
 def missingCaller = missing
 def self = [selfVar==1] self / "h"
 def assignment = [local=9] "i"`
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	got := ruleVariables(g.Rules())
	if want := ruleVariablesDFS(g.Rules()); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for name, want := range map[string][]string{
		"main": {"a", "m", "opt", "recoverVar", "rep", "z"},
		"pr":   {"atomVar", "inVar", "infixVar", "operandVar", "postVar", "postfixVar", "preVar", "prefixVar", "skipVar"},
		"self": {"selfVar"},
	} {
		if !reflect.DeepEqual(got[name], want) {
			t.Errorf("%s: got %v; want %v", name, got[name], want)
		}
	}
	for _, name := range []string{"missing", "missingCaller", "assignment"} {
		if _, ok := got[name]; ok {
			t.Errorf("unexpected empty-read entry %s", name)
		}
	}
}

func BenchmarkVariableDependencies(b *testing.B) {
	cases := []struct{ name, src string }{}
	for _, n := range []int{64, 512} {
		for _, reads := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				order := "caller-first"
				if reverse {
					order = "callee-first"
				}
				shape := "no-vars"
				if reads {
					shape = "leaf-var"
				}
				cases = append(cases, struct{ name, src string }{fmt.Sprintf("%s/%s-%d", shape, order, n), variableChain(n, reverse, reads)})
			}
		}
	}
	cases = append(cases, struct{ name, src string }{"shared/128", variableGraph(128, false)}, struct{ name, src string }{"cyclic/128", variableGraph(128, true)})
	cases = append(cases, struct{ name, src string }{"many-reads/chain-128", variableManyReads(128, false)}, struct{ name, src string }{"many-reads/cycle-128", variableManyReads(128, true)})
	for _, path := range []string{"../../examples/calculator/calc.pego", "../../examples/minilang/minilang.pego", "../../examples/outline/outline.pego", "../../parsers/json/json.pego"} {
		src, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		cases = append(cases, struct{ name, src string }{"control/" + path[strings.LastIndex(path, "/")+1:], string(src)})
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			g, err := syntax.Parse(c.src)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Compile(g, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
