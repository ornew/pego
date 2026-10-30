package runtime

import (
	"reflect"
	"testing"
)

// TestSharedGrammarInitialValues checks that the rules of a recognizer grammar, whose rule table is
// set before the variables of the grammar it shares tables with, key their memo entries by the
// initial values of their variables like the rules of that grammar do: a call made before
// any binding must find the entry made by a call after the same values were bound.
func TestSharedGrammarInitialValues(t *testing.T) {
	newRule := func(name string) *Rule { return &Rule{Name: name, Vars: []string{"s", "n", "ghost"}} }
	base := NewGrammar(0)
	base.SetRules([]*Rule{newRule("r")}, 0)
	rec := NewGrammar(0)
	rec.SetRules([]*Rule{newRule("r")}, 0)
	base.SetVars([]string{"n", "s"}, []any{3, "x"})
	rec.Share(base)

	for name, g := range map[string]*grammar{"base": base, "recognizer": rec} {
		r := g.ruleByName("r")
		want := []any{"x", 3, nil}
		if !reflect.DeepEqual(r.initVals, want) {
			t.Errorf("%s: initial values %v; want %v", name, r.initVals, want)
		}
		initial := &Parser{g: g, Env: g.initEnv, envDepth: g.envDepth}
		bound := &Parser{g: g, envDepth: g.envDepth}
		bound.Env = g.initEnv.set(0, g.envDepth, 3).set(1, g.envDepth, "x") // equal values in new nodes
		if got := initial.memoEnv(r); !reflect.DeepEqual(got, bound.memoEnv(r)) {
			t.Errorf("%s: initial key %v differs from the key of the same values bound: %v", name, got, bound.memoEnv(r))
		}
	}
}
