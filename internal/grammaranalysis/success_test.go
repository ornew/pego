package grammaranalysis

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

func proofJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPolledSuccessFacts(t *testing.T) {
	committed := &grammar.Seq{Items: []grammar.Expr{&grammar.Cut{}, &grammar.Literal{Value: "a"}}}
	for _, test := range []struct {
		name         string
		expr         grammar.Expr
		success, cut bool
	}{
		{"empty", &grammar.Literal{}, true, false},
		{"known call", &grammar.Ref{Name: "known"}, true, false},
		{"level call", &grammar.Ref{Name: "known", Level: "named"}, false, false},
		{"unknown call", &grammar.Ref{Name: "unknown"}, false, false},
		{"committed failure", committed, false, true},
		{"committed choice", &grammar.Choice{Alts: []grammar.Expr{committed, &grammar.Top{}}}, false, false},
		{"committed optional", &grammar.Optional{Expr: committed}, false, false},
		{"empty repeat", &grammar.Repeat{Expr: committed, Max: 0}, true, false},
		{"larger minimum", &grammar.Repeat{Expr: &grammar.Top{}, Min: 2, Max: 3}, false, false},
		{"negative failure", &grammar.Not{Expr: &grammar.Bottom{}}, true, false},
		{"nested negative", &grammar.Not{Expr: &grammar.Not{Expr: &grammar.Top{}}}, true, false},
		{"positive", &grammar.And{Expr: &grammar.Top{}}, true, false},
		{"group", &grammar.Group{Expr: &grammar.Cut{}}, true, true},
		{"capture", &grammar.Capture{Expr: &grammar.Cut{}}, true, true},
		{"atomic", &grammar.Atomic{Expr: &grammar.Cut{}}, true, true},
		{"discard", &grammar.Discard{Expr: &grammar.Cut{}}, true, true},
		{"stream", &grammar.Stream{Expr: &grammar.Cut{}}, true, true},
		{"recovery", &grammar.Recovery{Expr: &grammar.Bottom{}, Skip: &grammar.Cut{}}, false, true},
		{"recovery attribute", &grammar.Attributed{Expr: &grammar.Top{}, Attrs: []*grammar.Attribute{{Name: "recover", KwArgs: map[string]any{"skip": &grammar.Cut{}}}}}, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			sure := map[string]bool{"known": true}
			before := proofJSON(t, test.expr)
			polls := 0
			poll := func() { polls++ }
			for _, callback := range []func(){nil, poll} {
				if got := AlwaysSucceedsWithPoll(test.expr, sure, callback); got != test.success || got != AlwaysSucceeds(test.expr, sure) {
					t.Fatalf("success = %v, want %v", got, test.success)
				}
				if got := HasLooseCutWithPoll(test.expr, callback); got != test.cut || got != HasLooseCut(test.expr) {
					t.Fatalf("loose cut = %v, want %v", got, test.cut)
				}
			}
			after := proofJSON(t, test.expr)
			if polls == 0 || before != after || !reflect.DeepEqual(sure, map[string]bool{"known": true}) {
				t.Fatal("proof did not poll or modified its input")
			}
		})
	}
}

func TestPolledSuccessFixedPoint(t *testing.T) {
	rules := []*grammar.RuleDef{
		{Name: "main", Expr: &grammar.Ref{Name: "later"}},
		{Name: "later", Expr: &grammar.Top{}},
		{Name: "cycle", Expr: &grammar.Top{}},
	}
	cyclic := map[string]bool{"cycle": true}
	polls := 0
	got := SucceedingRulesWithPoll(rules, cyclic, func() { polls++ })
	want := map[string]bool{"main": true, "later": true}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got, SucceedingRules(rules, cyclic)) || polls == 0 || !reflect.DeepEqual(cyclic, map[string]bool{"cycle": true}) {
		t.Fatalf("facts = %v, polls = %d", got, polls)
	}
}

func TestSharedProofCancellation(t *testing.T) {
	tops, bottoms := make([]grammar.Expr, 1024), make([]grammar.Expr, 1024)
	attrs := make([]*grammar.Attribute, 1024)
	rules := make([]*grammar.RuleDef, 1024)
	cyclic := map[string]bool{}
	for i := range tops {
		tops[i], bottoms[i] = &grammar.Top{}, &grammar.Bottom{}
		attrs[i] = &grammar.Attribute{Name: "error"}
		name := fmt.Sprint(i)
		rules[i] = &grammar.RuleDef{Name: name, Expr: &grammar.Top{}}
		cyclic[name] = true // Excluded rules still require bounded polling.
	}
	for name, run := range map[string]func(func()){
		"success": func(p func()) { AlwaysSucceedsWithPoll(&grammar.Seq{Items: tops}, nil, p) },
		"failure": func(p func()) { AlwaysSucceedsWithPoll(&grammar.Not{Expr: &grammar.Choice{Alts: bottoms}}, nil, p) },
		"cut":     func(p func()) { HasLooseCutWithPoll(&grammar.Seq{Items: tops}, p) },
		"failure attributes": func(p func()) {
			AlwaysSucceedsWithPoll(&grammar.Not{Expr: &grammar.Attributed{Expr: &grammar.Bottom{}, Attrs: attrs}}, nil, p)
		},
		"cut attributes": func(p func()) { HasLooseCutWithPoll(&grammar.Attributed{Expr: &grammar.Top{}, Attrs: attrs}, p) },
		"excluded rules": func(p func()) { SucceedingRulesWithPoll(rules, cyclic, p) },
	} {
		t.Run(name, func(t *testing.T) {
			before := proofJSON(t, []any{tops, bottoms, attrs, rules, cyclic})
			token, polls := new(int), 0
			func() {
				defer func() {
					if recover() != token || polls != 17 {
						t.Fatal("cancellation was swallowed or changed")
					}
				}()
				run(func() {
					polls++
					if polls == 17 {
						panic(token)
					}
				})
				t.Fatal("proof finished without observing cancellation")
			}()
			after := proofJSON(t, []any{tops, bottoms, attrs, rules, cyclic})
			if before != after {
				t.Fatal("interrupted proof modified its input")
			}
		})
	}
}
