package engine

import (
	"fmt"
	"github.com/ornew/pego/internal/gentest"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
)

func TestInferenceDependencyChains(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{45, 50, 128, 512} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/reversed=%v", depth, reverse), func(t *testing.T) {
				g, err := parseTest(gentest.InferredChain(depth, reverse, ":node[string]"))
				if err != nil {
					t.Fatal(err)
				}
				_, err = Compile(g, compile.Options{})
				if err == nil || !strings.Contains(err.Error(), "produces P, which is not assignable to node[string]") {
					t.Errorf("long-chain type mismatch: %v", err)
				}
				p := mustCompile(t, gentest.InferredChain(depth, reverse, ""))
				for i := range depth {
					if got, ok := inferredType(p, fmt.Sprint("r", i)); !ok || got != "P" {
						t.Fatalf("r%d type = %q, %v; want P", i, got, ok)
					}
				}
				if got, ok := inferredType(p, "main"); !ok || got != "P" {
					t.Errorf("main type = %q, %v; want P", got, ok)
				}
			})
		}
	}
}

func TestInferenceKeepsIndependentTypes(t *testing.T) {
	t.Parallel()
	src := gentest.InferredChain(128, false, "") + `rule growing = xs:growing? => list($xs)
rule constant = "(" growing ")" => P{}`
	p := mustCompile(t, src)
	for _, name := range []string{"main", "r0", "r127", "constant"} {
		if got, _ := inferredType(p, name); got != "P" {
			t.Errorf("%s type = %s; want P", name, got)
		}
	}
	// A recursive type refers to the rule by name.
	if got, _ := inferredType(p, "growing"); got != "[]*growing" {
		t.Errorf("growing type = %s; want []*growing", got)
	}
}

func TestInferenceKeepsLargeFiniteRecursiveTypes(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	src.WriteString("rule main = recursive\nrule recursive = recursive / huge\nrule huge = ")
	for i := range 180 {
		fmt.Fprintf(&src, "field%d:\"a\" ", i)
	}
	src.WriteString("")
	p := mustCompile(t, src.String())
	want, _ := inferredType(p, "huge")
	if len(want) <= 2048 {
		t.Fatalf("test type is only %d bytes", len(want))
	}
	for _, name := range []string{"main", "recursive"} {
		if got, _ := inferredType(p, name); got != want {
			t.Errorf("%s lost a finite recursive type: %s", name, got)
		}
	}
}

func TestInferenceOperatorsRuleImplicitDependencies(t *testing.T) {
	t.Parallel()
	p := mustCompile(t, `type P = {}
rule main = expression
rule expression = #operators {
    operand "a"
    level { postfix "!" => list($lhs) }
}
rule constant = "a" => P{}`)
	if got, _ := inferredType(p, "expression"); got != "node[string] | []expression" {
		t.Errorf("recursive OperatorsRule operator type = %s; want node[string] | []expression", got)
	}
	if got, _ := inferredType(p, "constant"); got != "P" {
		t.Errorf("constant type = %s; want P", got)
	}
}

func TestInferenceKeepsConstantRecursivePeer(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		rules := []string{`rule growing = xs:(growing? / constant) => list($xs)`, `rule constant = "(" growing ")" => P{}`}
		if reverse {
			rules[0], rules[1] = rules[1], rules[0]
		}
		p := mustCompile(t, "type P = {}\nrule main = constant\n"+strings.Join(rules, "\n"))
		for _, name := range []string{"main", "constant"} {
			if got, _ := inferredType(p, name); got != "P" {
				t.Errorf("%s type = %s; want P", name, got)
			}
		}
	}
}

func TestInferenceFiniteRecursiveChains(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	src.WriteString("rule main = r0\n")
	for i := range 128 {
		if i == 127 {
			fmt.Fprintf(&src, "rule r%d = \"a\" / r0\n", i)
		} else {
			fmt.Fprintf(&src, "rule r%d = r%d\n", i, i+1)
		}
	}
	p := mustCompile(t, src.String())
	for i := range 128 {
		if got, _ := inferredType(p, fmt.Sprint("r", i)); got != "node[string]" {
			t.Errorf("r%d type = %s; want node[string]", i, got)
		}
	}
}

func BenchmarkInferenceDependencies(b *testing.B) {
	for _, c := range []struct{ name, source string }{
		{"caller-first-64", gentest.InferredChain(64, false, "")},
		{"callee-first-64", gentest.InferredChain(64, true, "")},
		{"caller-first-512", gentest.InferredChain(512, false, "")},
		{"growing-list", `rule main = xs:main? => list($xs)`},
	} {
		b.Run(c.name, func(b *testing.B) {
			g, err := parseTest(c.source)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Compile(g, compile.Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestInferenceFiniteJoinCycle(t *testing.T) {
	t.Parallel()
	const count = 64
	var src strings.Builder
	src.WriteString("rule main = r0\n")
	for i := range count {
		fmt.Fprintf(&src, "type T%d = { Text string }\nrule n%d T%d = t:\"a\" #atomic => T%d{Text: text($t)}\nrule r%d = r%d / r%d / n%d\n", i, i, i, i, i, (i+1)%count, (i+count-1)%count, i)
	}
	p := mustCompile(t, src.String())
	for i := range count {
		// RuleType shows the rules in a cycle by name.
		got, _ := inferredType(p, fmt.Sprint("r", i))
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
	t.Parallel()
	p := mustCompile(t, gentest.InferredChain(64, false, ""))
	for _, mode := range valueModes {
		q := saveLoad(t, p, "main", mode)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if backend == Closure {
				_, err := q.ParseWith("main", "a", servedBy(mode, ParseOptions{Backend: backend}))
				wantClosureRefused(t, err)
				continue
			}
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				n, err := q.ParseWith("main", "a", servedBy(mode, ParseOptions{Backend: backend, Unit: unit}))
				if err != nil || n.String() != "P{}" {
					t.Errorf("saved %s/%s result: %v, %v", backend, unit, n, err)
				}
			}
		}
	}
}

func TestInferenceChainFieldMismatch(t *testing.T) {
	t.Parallel()
	src := "type Holder = { Value node[string] }\n" + strings.Replace(gentest.InferredChain(128, false, ""), "rule main = r0", "rule main = r0:r0 => Holder{Value: $r0}", 1)
	g, err := parseTest(src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(g, compile.Options{})
	if err == nil || !strings.Contains(err.Error(), "cannot use P as node[string] in field Value of Holder") {
		t.Errorf("long-chain field mismatch: %v", err)
	}
}

// inferredType returns the value type the checker infers for the rule name, with the rules it
// refers to replaced by their values at the top level and in unions and optional types. RuleType
// refers to a rule without a declared type by name instead.
func inferredType(p *Program, name string) (string, bool) {
	t, ok := p.Typed.Values[name]
	if !ok {
		return "", false
	}
	v := &compile.ValueInference{Prog: p.Static(), Out: p.Typed.Values}
	return v.Expand(t).String(), true
}

// captureType returns the type of the capture cap in the value of the rule name, after checkTypes:
// the union of the types of all captures of that name.
func captureType(p *Program, name, cap string) (string, bool) {
	t, ok := p.Typed.Values[name]
	if !ok {
		return "", false
	}
	v := &compile.ValueInference{Prog: p.Static(), Out: p.Typed.Values}
	n, ok := v.Expand(t).(compile.NodeTy)
	if !ok {
		return "", false
	}
	ct, ok := compile.CapType(n.Caps, cap)
	if !ok {
		return "", false
	}
	return ct.String(), true
}
