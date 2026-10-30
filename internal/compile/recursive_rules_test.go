package compile

import "testing"

// A rule counts against the nesting limit exactly when it is on a cycle of calls, or calls itself;
// the value-free twin of a rule is as recursive as the rule.
func TestRecursiveRules(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = a ";" leaf $$
rule a = "(" a ")" / b / "x"
rule b = "[" c "]"
rule c = a / "y"
rule self = "s" self?
rule leaf = tail
rule tail = "t"
rule ops = #operators {
    operand "x"
    level { prefix "-" }
}
rule viaops = ops`)
	for name, want := range map[string]bool{
		"main": false, "a": true, "b": true, "c": true, "self": true,
		"leaf": false, "tail": false, "ops": false, "viaops": false,
	} {
		r := prog.ByName[name]
		if r == nil {
			t.Fatalf("no rule %s", name)
		}
		if r.Rec != want {
			t.Errorf("rule %s: Rec = %v, want %v", name, r.Rec, want)
		}
		if r.Twin != nil && r.Twin.Rec != want {
			t.Errorf("twin of rule %s: Rec = %v, want %v", name, r.Twin.Rec, want)
		}
	}
	for _, tw := range prog.Twins {
		if tw.Rec != prog.ByName[tw.Name].Rec {
			t.Errorf("twin of rule %s: Rec = %v", tw.Name, tw.Rec)
		}
	}
}
