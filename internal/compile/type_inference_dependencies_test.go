package compile

import (
	"fmt"
	"strings"
	"testing"
)

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
