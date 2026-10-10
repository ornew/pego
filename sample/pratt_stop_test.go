package sample

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego"
)

// Pending scopes can outlive later choices and caller continuations. Their immutable state remains
// independent, and different frame states must not grow the static entry-level cache.
func TestPrattStopCheckOwnershipAndCacheBound(t *testing.T) {
	const levels = 70
	var src strings.Builder
	src.WriteString("def main = e $$\ndef e = pratt { operand \"a\" ")
	for i := range levels {
		fmt.Fprintf(&src, "level { infix none \"<%d>\" } ", i)
	}
	src.WriteByte('}')
	ast, err := pego.ParseGrammar(src.String())
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	ri := in.rules["e"]
	g := newGen(in, &config{})
	closed := newPrattScope(0, levels-1, nil)
	first := g.prattStopCheck(ri, closed)
	m := matcher{in: in}
	for n := range 256 {
		st, _ := m.runPrattStop(first, []byte("<69>a"), 0, true)
		if st != failed {
			t.Fatalf("previous scope changed: %v", st)
		}
		current := newPrattScope(0, n%(levels-1), nil)
		c := g.prattStopCheck(ri, current)
		st, _ = m.runPrattStop(c, []byte("<69>a"), 0, true)
		if st != matched {
			t.Fatalf("current scope: %v", st)
		}
		if len(ri.stops) != 1 {
			t.Fatalf("static cache grew with scopes: %d", len(ri.stops))
		}
	}
}

func TestPrattPendingNestedScopes(t *testing.T) {
	ast, err := pego.ParseGrammar(`def main=e $$ def e=pratt { operand "a" level { infix none "<" } level { infix none "=" } }`)
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	g := newGen(in, &config{})
	ri := in.rules["e"]
	root := newPrattScope(0, 0, nil)
	inner := newPrattScope(1, 1, root)
	deepest := newPrattScope(2, -1, inner)
	saved := g.prattStopCheck(ri, deepest)
	m := matcher{in: in}
	for range 128 {
		// A later branch resets a different root; it must not change a saved pending scope.
		later := g.prattStopCheck(ri, newPrattScope(0, -1, nil))
		st, _ := m.runPrattStop(later, []byte("<a"), 0, true)
		if st != matched {
			t.Fatal("fresh scope did not admit <")
		}
		st, _ = m.runPrattStop(saved, []byte("<a"), 0, true)
		if st != failed {
			t.Fatal("nested scope changed parent none restriction")
		}
		st, _ = m.runPrattStop(saved, []byte("=a"), 0, true)
		if st != matched {
			t.Fatal("nested none should return to an accepting parent")
		}
	}
	if len(ri.stops) != 1 {
		t.Fatalf("cache grew with scopes: %d", len(ri.stops))
	}
}

func TestPrattScopeWorkBudget(t *testing.T) {
	ast, err := pego.ParseGrammar(`def main=e $$ def e=pratt { operand "a" level { postfix "!" } }`)
	if err != nil {
		t.Fatal(err)
	}
	in := analyze(ast, "main")
	g := newGen(in, &config{budget: 2})
	op := prattOp{op: in.rules["e"].pratt.Levels[0].Operators[0]}
	scope := newPrattScope(0, -1, nil)
	for range 3 {
		scope = newPrattScope(1, -1, scope)
	}
	if _, work, complete := scope.owner(op, 2); complete || work != 3 {
		t.Fatalf("owner work=%d complete=%t", work, complete)
	}
	if owner := g.prattOwner(scope, op); owner != nil || !g.exhausted() {
		t.Fatalf("generation owner=%v steps=%d", owner, g.steps)
	}
	deep := newPrattScope(1, -1, nil)
	for range matchSteps {
		deep = newPrattScope(1, -1, deep)
	}
	c := g.prattStopCheck(in.rules["e"], deep)
	m := matcher{in: in}
	st, _ := m.runPrattStop(c, []byte("!"), 0, true)
	if st != unknown || m.steps != matchSteps+1 {
		t.Fatalf("matching status=%v steps=%d", st, m.steps)
	}
}
