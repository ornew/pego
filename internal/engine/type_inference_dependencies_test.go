package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func inferredChain(depth int, reverse bool, mainType string) string {
	var src strings.Builder
	src.WriteString("type P struct {}\n")
	fmt.Fprintf(&src, "def main%s = r0\n", mainType)
	for j := range depth {
		i := j
		if reverse {
			i = depth - 1 - j
		}
		if i == depth-1 {
			fmt.Fprintf(&src, "def r%d = \"a\" -> new P{}\n", i)
		} else {
			fmt.Fprintf(&src, "def r%d = r%d\n", i, i+1)
		}
	}
	return src.String()
}

func TestInferenceDependencyChains(t *testing.T) {
	for _, depth := range []int{45, 50, 128, 512} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/reversed=%v", depth, reverse), func(t *testing.T) {
				g, err := syntax.Parse(inferredChain(depth, reverse, ":Match"))
				if err != nil {
					t.Fatal(err)
				}
				_, err = Compile(g, Options{})
				if err == nil || !strings.Contains(err.Error(), "produces P, which is not assignable to Match") {
					t.Errorf("long-chain type mismatch: %v", err)
				}
				p := compile(t, inferredChain(depth, reverse, ""))
				for i := range depth {
					if got, ok := p.RuleType(fmt.Sprint("r", i)); !ok || got != "P" {
						t.Fatalf("r%d type = %q, %v; want P", i, got, ok)
					}
				}
				if got, ok := p.RuleType("main"); !ok || got != "P" {
					t.Errorf("main type = %q, %v; want P", got, ok)
				}
			})
		}
	}
}

func TestInferenceKeepsIndependentTypes(t *testing.T) {
	src := inferredChain(128, false, "") + `
def growing = xs:growing? -> list($xs)
def constant = "(" growing ")" -> new P{}`
	p := compile(t, src)
	for _, name := range []string{"main", "r0", "r127", "constant"} {
		if got, _ := p.RuleType(name); got != "P" {
			t.Errorf("%s type = %s; want P", name, got)
		}
	}
	if got, _ := p.RuleType("growing"); got != "any" {
		t.Errorf("growing type = %s; want bounded inference to use any", got)
	}
}

func TestInferenceKeepsLargeFiniteRecursiveTypes(t *testing.T) {
	var src strings.Builder
	src.WriteString("def main = recursive\ndef recursive = recursive / huge\ndef huge = ")
	for i := range 180 {
		fmt.Fprintf(&src, "field%d:\"a\" ", i)
	}
	p := compile(t, src.String())
	want, _ := p.RuleType("huge")
	if len(want) <= maxInferredType {
		t.Fatalf("test type is only %d bytes", len(want))
	}
	for _, name := range []string{"main", "recursive"} {
		if got, _ := p.RuleType(name); got != want {
			t.Errorf("%s lost a finite recursive type: %s", name, got)
		}
	}
}

func TestInferencePrattImplicitDependencies(t *testing.T) {
	p := compile(t, `type P struct {}
def main = expression
def expression = pratt { operand "a" level { postfix "!" -> list($lhs) } }
def constant = "a" -> new P{}`)
	if got, _ := p.RuleType("expression"); got != "any" {
		t.Errorf("recursive Pratt operator type = %s; want bounded inference to use any", got)
	}
	if got, _ := p.RuleType("constant"); got != "P" {
		t.Errorf("constant type = %s; want P", got)
	}
}

func TestInferenceKeepsConstantRecursivePeer(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		rules := []string{`def growing = xs:(growing? / constant) -> list($xs)`, `def constant = "(" growing ")" -> new P{}`}
		if reverse {
			rules[0], rules[1] = rules[1], rules[0]
		}
		p := compile(t, "type P struct {}\ndef main = constant\n"+strings.Join(rules, "\n"))
		for _, name := range []string{"main", "constant"} {
			if got, _ := p.RuleType(name); got != "P" {
				t.Errorf("%s type = %s; want P", name, got)
			}
		}
	}
}

func TestInferenceFiniteRecursiveChains(t *testing.T) {
	var src strings.Builder
	src.WriteString("def main = r0\n")
	for i := range 128 {
		if i == 127 {
			fmt.Fprintf(&src, "def r%d = \"a\" / r0\n", i)
		} else {
			fmt.Fprintf(&src, "def r%d = r%d\n", i, i+1)
		}
	}
	p := compile(t, src.String())
	for i := range 128 {
		if got, _ := p.RuleType(fmt.Sprint("r", i)); got != "Match" {
			t.Errorf("r%d type = %s; want Match", i, got)
		}
	}
}

func BenchmarkInferenceDependencies(b *testing.B) {
	for _, c := range []struct{ name, source string }{
		{"caller-first-64", inferredChain(64, false, "")},
		{"callee-first-64", inferredChain(64, true, "")},
		{"caller-first-512", inferredChain(512, false, "")},
		{"growing-list", `def main = xs:main? -> list($xs)`},
	} {
		b.Run(c.name, func(b *testing.B) {
			g, err := syntax.Parse(c.source)
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

func TestInferenceFiniteUnionCycle(t *testing.T) {
	const count = 64
	var src strings.Builder
	src.WriteString("def main = r0\n")
	for i := range count {
		fmt.Fprintf(&src, "type T%d terminal\ndef n%d: T%d = \"a\"\ndef r%d = r%d / r%d / n%d\n", i, i, i, i, (i+1)%count, (i+count-1)%count, i)
	}
	p := compile(t, src.String())
	for i := range count {
		got, _ := p.RuleType(fmt.Sprint("r", i))
		members := strings.Split(got, " | ")
		if len(members) != count {
			t.Errorf("r%d type has %d members; want %d: %s", i, len(members), count, got)
			continue
		}
		set := map[string]bool{}
		for _, member := range members {
			set[member] = true
		}
		for j := range count {
			if !set[fmt.Sprint("T", j)] {
				t.Errorf("r%d lacks T%d", i, j)
			}
		}
	}
}

func TestInferenceChainSavedValues(t *testing.T) {
	p := compile(t, inferredChain(64, false, ""))
	for _, marshal := range []MarshalOptions{{}, {OmitAST: true}} {
		data, err := p.MarshalBinaryWith("main", marshal)
		if err != nil {
			t.Fatal(err)
		}
		q, _, err := LoadProgram(data, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if marshal.OmitAST && backend == Closure {
				continue
			}
			for _, unit := range []Unit{CodePoints, Bytes} {
				n, err := q.ParseWith("main", "a", ParseOptions{Backend: backend, Unit: unit})
				if err != nil || n.String() != "(P)" {
					t.Errorf("saved %s/%s result: %v, %v", backend, unit, n, err)
				}
			}
		}
	}
}

func TestInferenceCanonicalTypes(t *testing.T) {
	p := namedTy{"P", 's'}
	a := recordTy{map[string]ty{"field": listTy{unionTy{[]ty{tyMatch, p}}}}}
	b := recordTy{map[string]ty{"field": listTy{unionTy{[]ty{p, tyMatch}}}}}
	if a.String() == b.String() {
		t.Fatal("test needs different public union orders")
	}
	if inferredShape(a).key != inferredShape(b).key {
		t.Fatal("equivalent nested unions have different keys")
	}
	duplicate := unionTy{[]ty{a, b}}
	if inferredShape(duplicate).key != inferredShape(a).key {
		t.Fatal("equivalent union members are not deduplicated")
	}
	for _, value := range []ty{a, b, duplicate, listTy{duplicate}, optTy{duplicate}} {
		if got := inferredShape(value).size; got != len(value.String()) {
			t.Errorf("written size of %s = %d, want %d", value, got, len(value.String()))
		}
	}
	if inferredShape(tyMatch).key == inferredShape(namedTy{"Match", 't'}).key {
		t.Fatal("distinct named kinds collide")
	}
	if inferredShape(recordTy{map[string]ty{"a": namedTy{"b:c", 's'}}}).key == inferredShape(recordTy{map[string]ty{"a:b": namedTy{"c", 's'}}}).key {
		t.Fatal("unframed field/type names collide")
	}
}

func TestInferenceChainFieldMismatch(t *testing.T) {
	src := "type Holder struct { Value Match }\n" + strings.Replace(inferredChain(128, false, ""), "def main = r0", "def main = r0 -> new Holder{Value: $1}", 1)
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(g, Options{})
	if err == nil || !strings.Contains(err.Error(), "cannot use P as Match in field Value of Holder") {
		t.Errorf("long-chain field mismatch: %v", err)
	}
}
