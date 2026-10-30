package engine

import (
	"testing"
)

func TestRuleType(t *testing.T) {
	t.Parallel()
	// A reference to a rule with a declared type shows that type; one to a rule without a declared
	// type shows the rule's name.
	prog := mustCompile(t, `type N = { Text string }
type P = { X N }
rule n N = t:("1" #atomic) => N{Text: text($t)}
rule nn = n
rule p = x:n => P{X: $x}
rule ps = p*
rule pl = xs:p* => $xs.Value
rule opt = "a"?
rule on *N = n?
rule oo = on?
rule any node[_] = "a" "b"
rule main = ps any $$`)
	for name, want := range map[string]string{
		"n": "N", "nn": "N", "p": "P", "ps": "node[[]p]", "pl": "[]p", "opt": "*node[string]", "on": "*N", "oo": "*N",
		"any": "node[_]", "main": "node[(ps, node[_])]",
	} {
		if got, ok := prog.RuleType(name); !ok || got != want {
			t.Errorf("RuleType(%s) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := prog.RuleType("nope"); ok {
		t.Error("RuleType reported an undefined rule")
	}
	data, err := mustCompile(t, `rule main = "a"`).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.RuleType("main"); ok {
		t.Error("RuleType reported a type for a program loaded from a compiled grammar")
	}
}

