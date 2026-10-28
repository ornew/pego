package runtime

import (
	"reflect"
	"testing"
)

func TestNewRules(t *testing.T) {
	sh := Shaper{Elems: true, K: 2, Mode: 1, Tuple: true}
	strs := [][]string{nil, {"a", "b"}, {"x"}}
	specs := []RuleSpec{
		{Name: "r0"},
		{Name: "r1", Value: &sh, Flags: RuleMemo | RuleLeader | RuleDocMemo, Scope: 1, Vars: 2},
		{Name: "r2", Flags: RuleNovalue | RuleSplice, Seen: 4},
		{Name: "r3", Flags: RuleMemo, Seen: 1},
	}
	extras := []RuleExtra{{Id: 2, Where: "w", NodeType: "N", OpNodeType: "O"}}
	rules := NewRules(specs, strs, extras)
	if len(rules) != 4 {
		t.Fatalf("got %d rules", len(rules))
	}
	for i, r := range rules {
		if r.Id != i || r.Name != specs[i].Name {
			t.Errorf("rule %d: id %d name %q", i, r.Id, r.Name)
		}
	}
	r := rules[1]
	if r.Value != sh || !r.Memo || !r.Leader || !r.DocMemo || r.Novalue || r.Splice || r.Seen != -1 ||
		!reflect.DeepEqual(r.Scope, []string{"a", "b"}) || !reflect.DeepEqual(r.Vars, []string{"x"}) {
		t.Errorf("rule 1 = %+v", r)
	}
	r = rules[2]
	if r.Value != (Shaper{}) || r.Memo || !r.Novalue || !r.Splice || r.Seen != 3 || r.Where != "w" || r.NodeType != "N" || r.OpNodeType != "O" {
		t.Errorf("rule 2 = %+v", r)
	}
	if rules[3].Seen != 0 {
		t.Errorf("a spec with Seen 1 is the first deferred rule: Seen %d", rules[3].Seen)
	}
	if rules[0].Seen != -1 || rules[0].Where != "" || rules[0].NodeType != "" {
		t.Errorf("rule 0 = %+v", rules[0])
	}
	tr := NewTRules(rules)
	for i := range tr {
		if tr[i].Rule != rules[i] {
			t.Errorf("typed rule %d does not share rule %d", i, i)
		}
	}
}
