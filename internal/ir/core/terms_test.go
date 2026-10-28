package core

import "testing"

func txt(x Term) Term { return &Builtin{Func: "text", Args: []Term{x}} }

func str(s string) Term { return &Const{Value: s} }

func TestRewriteTerms(t *testing.T) {
	x := &SlotRef{Slot: 0, Name: "x"}
	y := &SlotRef{Slot: 1, Name: "y"}
	param := &Lambda{Params: []string{"e"}}
	param.Body = &Member{X: &Local{Kind: LocalParam, Name: "e", Lambda: param}, Name: "r"}
	cases := []struct {
		name string
		in   Term
		pred bool
		opts TermOptions
		want string
	}{
		{"text equality", &Binary{Op: "!=", L: txt(x), R: txt(y)}, false, TermOptions{}, "text-eq($0:x != $1:y)"},
		{"field compare", &Binary{Op: "<", L: &Member{X: x, Name: "N"}, R: &Const{Value: 3}}, false, TermOptions{FieldCompare: true}, "field-cmp($0:x.N < 3)"},
		{"boxed compare", &Binary{Op: "<", L: &Member{X: x, Name: "N"}, R: &Const{Value: 3}}, false, TermOptions{}, "($0:x.N < 3)"},
		{"node member", &Binary{Op: "<", L: &Member{X: x, Name: "$o"}, R: &Const{Value: 3}}, false, TermOptions{FieldCompare: true}, "($0:x.$o < 3)"},
		{"text dispatch", &Binary{Op: "||", L: &Binary{Op: "==", L: txt(x), R: str("a")}, R: &Binary{Op: "||", L: &Binary{Op: "==", L: str("b"), R: txt(x)}, R: &Binary{Op: "==", L: txt(x), R: str("a")}}}, true, TermOptions{TextDispatch: true}, `text-in($0:x, "a" "b")`},
		{"text dispatch in an action", &Binary{Op: "||", L: &Binary{Op: "==", L: txt(x), R: str("a")}, R: &Binary{Op: "==", L: txt(x), R: str("b")}}, false, TermOptions{TextDispatch: true}, `((text($0:x) == "a") || (text($0:x) == "b"))`},
		{"one literal", &Binary{Op: "==", L: txt(x), R: str("a")}, true, TermOptions{TextDispatch: true}, `(text($0:x) == "a")`},
		{"two captures", &Binary{Op: "||", L: &Binary{Op: "==", L: txt(x), R: str("a")}, R: &Binary{Op: "==", L: txt(y), R: str("b")}}, true, TermOptions{TextDispatch: true}, `((text($0:x) == "a") || (text($1:y) == "b"))`},
		{"concat", &Builtin{Func: "concat", Args: []Term{&Builtin{Func: "list", Args: []Term{x}}, &Builtin{Func: "concat", Args: []Term{&Builtin{Func: "map", Args: []Term{y, param}, Projected: true}}}, &Builtin{Func: "map", Args: []Term{y, param}}}}, false, TermOptions{}, "concat-fused(list($0:x), items($1:y), map($1:y, fn(e) param:e.r))"},
		{"concat of a capture", &Builtin{Func: "concat", Args: []Term{&Builtin{Func: "list", Args: []Term{x}}, y}}, false, TermOptions{}, "concat(list($0:x), $1:y)"},
		{"projected map", &Builtin{Func: "map", Args: []Term{y, param}, Projected: true}, false, TermOptions{}, "projected-map($1:y)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := rule(0, "a", lit("a"), "x", "y")
			if c.pred {
				r.Body = &Pred{Term: c.in}
			} else {
				r.Finish = Finish{Kind: FinishAction, Action: c.in}
			}
			typed(r, c.in)
			m := module(r)
			m.Typed = true
			RewriteTerms(m, c.opts)
			got := r.Finish.Action
			if c.pred {
				got = r.Body.(*Pred).Term
			}
			if s := TermString(got); s != c.want {
				t.Errorf("got %s, want %s", s, c.want)
			}
			// Every term the rewrites make has the type of the term it replaces.
			analyzed(t, m)
			if TypeOf(got) != TypeOf(c.in) {
				t.Errorf("type %v, want %v", TypeOf(got), TypeOf(c.in))
			}
		})
	}
}

// typed gives the rule, its slots and every term of t but lambdas a distinct type.
func typed(r *Rule, t Term) {
	r.Type = "rule"
	for i := range r.Scope {
		r.Scope[i].Type = "slot " + r.Scope[i].Name
	}
	n := 0
	WalkTerm(t, func(x Term) {
		if _, lambda := x.(*Lambda); !lambda {
			n++
			SetType(x, n)
		}
	})
}

func TestRewriteTermsFieldOrder(t *testing.T) {
	for _, c := range []struct {
		given []string
		want  bool
	}{{nil, true}, {[]string{"A", "C"}, true}, {[]string{"C", "A"}, false}, {[]string{"A", "D"}, false}} {
		n := &New{Type: "T", Declared: []string{"A", "B", "C"}}
		for _, f := range c.given {
			n.Fields = append(n.Fields, FieldInit{Name: f, Value: &Const{Value: 1}})
		}
		r := rule(0, "a", lit("a"))
		r.Finish = Finish{Kind: FinishAction, Action: n}
		RewriteTerms(&Module{Rules: []*Rule{r}}, TermOptions{})
		if n.InOrder != c.want {
			t.Errorf("fields %v: in order %v", c.given, n.InOrder)
		}
	}
}
