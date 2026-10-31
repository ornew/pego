package sample

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse"
)

// Pending scopes can outlive later choices and caller continuations. Their immutable state remains
// independent, and different frame states must not grow the static entry-level cache.
func TestOperatorsRuleStopCheckOwnershipAndCacheBound(t *testing.T) {
	const levels = 70
	var src strings.Builder
	src.WriteString("rule main = e $$\nrule e = #operators { operand \"a\"; ")
	for i := range levels {
		fmt.Fprintf(&src, "level { infix none \"<%d>\" }; ", i)
	}
	src.WriteByte('}')
	ast, err := zeroparse.ParseGrammar(src.String())
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	ri := in.rules["e"]
	g := newGen(in, &config{})
	closed := newOperatorsRuleScope(0, levels-1, nil)
	first := g.operatorsRuleStopCheck(ri, closed)
	m := matcher{in: in}
	for n := range 256 {
		st, _ := m.runOperatorsRuleStop(first, []byte("<69>a"), 0, true)
		if st != failed {
			t.Fatalf("previous scope changed: %v", st)
		}
		current := newOperatorsRuleScope(0, n%(levels-1), nil)
		c := g.operatorsRuleStopCheck(ri, current)
		st, _ = m.runOperatorsRuleStop(c, []byte("<69>a"), 0, true)
		if st != matched {
			t.Fatalf("current scope: %v", st)
		}
		if len(ri.stops) != 1 {
			t.Fatalf("static cache grew with scopes: %d", len(ri.stops))
		}
	}
}

func TestOperatorsRulePendingNestedScopes(t *testing.T) {
	ast, err := zeroparse.ParseGrammar(`rule main = e $$
rule e = #operators {
    operand "a"
    level { infix none "<" }
    level { infix none "=" }
}`)
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	g := newGen(in, &config{})
	ri := in.rules["e"]
	root := newOperatorsRuleScope(0, 0, nil)
	inner := newOperatorsRuleScope(1, 1, root)
	deepest := newOperatorsRuleScope(2, -1, inner)
	saved := g.operatorsRuleStopCheck(ri, deepest)
	m := matcher{in: in}
	for range 128 {
		// A later branch resets a different root; it must not change a saved pending scope.
		later := g.operatorsRuleStopCheck(ri, newOperatorsRuleScope(0, -1, nil))
		st, _ := m.runOperatorsRuleStop(later, []byte("<a"), 0, true)
		if st != matched {
			t.Fatal("fresh scope did not admit <")
		}
		st, _ = m.runOperatorsRuleStop(saved, []byte("<a"), 0, true)
		if st != failed {
			t.Fatal("nested scope changed parent none restriction")
		}
		st, _ = m.runOperatorsRuleStop(saved, []byte("=a"), 0, true)
		if st != matched {
			t.Fatal("nested none should return to an accepting parent")
		}
	}
	if len(ri.stops) != 1 {
		t.Fatalf("cache grew with scopes: %d", len(ri.stops))
	}
}

func TestOperatorsRuleScopeBudgetBudget(t *testing.T) {
	ast, err := zeroparse.ParseGrammar(`rule main = e $$
rule e = #operators {
    operand "a"
    level { postfix "!" }
}`)
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	g := newGen(in, &config{budget: 2})
	op := operatorsRuleOp{op: in.rules["e"].operatorsRule.Levels[0].Operators[0]}
	scope := newOperatorsRuleScope(0, -1, nil)
	for range 3 {
		scope = newOperatorsRuleScope(1, -1, scope)
	}
	if _, work, complete := scope.owner(op, 2); complete || work != 3 {
		t.Fatalf("owner work=%d complete=%t", work, complete)
	}
	if owner := g.operatorsRuleOwner(scope, op); owner != nil || !g.exhausted() {
		t.Fatalf("generation owner=%v steps=%d", owner, g.steps)
	}
	deep := newOperatorsRuleScope(1, -1, nil)
	for range matchSteps {
		deep = newOperatorsRuleScope(1, -1, deep)
	}
	c := g.operatorsRuleStopCheck(in.rules["e"], deep)
	m := matcher{in: in}
	st, _ := m.runOperatorsRuleStop(c, []byte("!"), 0, true)
	if st != unknown || m.steps != matchSteps+1 {
		t.Fatalf("matching status=%v steps=%d", st, m.steps)
	}
}
