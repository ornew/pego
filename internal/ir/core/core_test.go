package core

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func lit(s string) *Lit { return &Lit{Text: s} }

func class(lo, hi rune) *ClassNode {
	return &ClassNode{Class: &Class{Ranges: []Range{{lo, hi}}}}
}

func seq(items ...Node) *Seq { return &Seq{Items: items} }

func choice(bodies ...Node) *Choice {
	c := &Choice{}
	for _, b := range bodies {
		c.Alts = append(c.Alts, &Alt{Body: b})
	}
	return c
}

func slots(names ...string) []Slot {
	var s []Slot
	for _, n := range names {
		s = append(s, Slot{Name: n})
	}
	return s
}

func rule(id int, name string, body Node, scope ...string) *Rule {
	return &Rule{ID: id, Name: name, Body: body, Want: true, Scope: slots(scope...), Policy: Policy{Seen: -1}}
}

// module makes a module of the rules, as the builder does: it interns the expectations of the
// literals and labels and the classes, and sets the call graph. A label's expectation is
// "label".
func module(rules ...*Rule) *Module {
	m := &Module{Rules: rules}
	ids := map[string]ExpID{}
	exp := func(s string) ExpID {
		id, ok := ids[s]
		if !ok {
			id = ExpID(len(m.Exps))
			ids[s] = id
			m.Exps = append(m.Exps, s)
		}
		return id
	}
	seen := map[*Class]bool{}
	intern := func(c *Class) {
		if !seen[c] {
			seen[c] = true
			c.Exp = exp(classString(c))
			m.Classes = append(m.Classes, c)
		}
	}
	for _, r := range rules {
		if r == nil {
			continue
		}
		Walk(r.Body, func(n Node) {
			switch n := n.(type) {
			case *Lit:
				if n.Text != "" {
					n.Exp = exp(strconv.Quote(n.Text))
				}
			case *ClassNode:
				intern(n.Class)
			case *Label:
				n.Exp = exp("label")
			case *Scan:
				if n.Class != nil {
					intern(n.Class)
				}
			case *ClassRun:
				for _, c := range n.Classes {
					intern(c)
				}
			}
		})
	}
	CallGraph(m)
	for i, r := range m.Rules {
		if r != nil { // the rules on a cycle of calls count against the nesting limit
			r.Policy.Rec = m.Cyclic[m.SCC[i]]
		}
	}
	return m
}

// analyzed runs Analyze and Verify on m.
func analyzed(t *testing.T, m *Module) *Module {
	t.Helper()
	Analyze(m)
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAnalyzeNullableAndEffects(t *testing.T) {
	// a = x:b ("c" / #cut d)?;  b = [0-9]* ;  d = &"e" {v == 1}
	pred := &Pred{Term: &Binary{Op: "==", L: &VarRef{Name: "v"}, R: &Const{Value: 1}}}
	cut := &Cut{}
	opt := &Opt{Body: choice(lit("c"), seq(cut, &Call{Rule: 2}))}
	capt := &Capture{Slot: 0, Name: "x", Body: &Call{Rule: 1}}
	a := seq(capt, opt)
	m := analyzed(t, module(
		rule(0, "a", a, "x"),
		rule(1, "b", &Repeat{Min: 0, Max: -1, Body: class('0', '9')}),
		rule(2, "d", seq(&Look{Body: lit("e")}, pred)),
	))
	for _, c := range []struct {
		n        Node
		nullable bool
		has, not Effect
	}{
		{a, true, EffConsume | EffExpect | EffCapture | EffCall | EffRaise, EffFail | EffCut | EffBind},
		{capt, true, EffCapture | EffConsume | EffCall, EffFail},
		{opt, true, EffConsume | EffExpect | EffCall | EffRaise, EffFail | EffCut},
		{cut, true, EffCut, 0},
		{m.Rules[1].Body, true, EffConsume | EffExpect, EffFail | EffCapture},
		{m.Rules[2].Body, true, EffFail | EffRaise, EffConsume | EffExpect},
	} {
		at := c.n.Attrs()
		if at.Nullable != c.nullable || at.Eff.Set&c.has != c.has || at.Eff.Set&c.not != 0 {
			t.Errorf("%s: nullable=%v effects %s; want nullable=%v, has %s, not %s",
				strings.TrimSpace(PrintNode(m, c.n, false)), at.Nullable, effectString(at.Eff), c.nullable,
				effectString(Effects{Set: c.has}), effectString(Effects{Set: c.not}))
		}
	}
	if !slices.Equal(a.Attrs().Eff.Slots, []int{0}) {
		t.Errorf("slots %v, want [0]", a.Attrs().Eff.Slots)
	}
}

// TestAnalyzeScopedRepetition checks that a repetition element with captures of its own keeps
// its captures and lets its bindings through: a rule invocation is the only variable scope.
func TestAnalyzeScopedRepetition(t *testing.T) {
	bind := &BindNode{Var: "v", Term: &Const{Value: 1}}
	rep := &Repeat{Max: -1, Scope: slots("x"), Body: seq(&Capture{Slot: 0, Name: "x", Body: lit("a")}, bind)}
	analyzed(t, module(rule(0, "a", rep)))
	if e := rep.Attrs().Eff; e.Set&EffBind == 0 || e.Set&EffCapture != 0 || len(e.Slots) != 0 {
		t.Errorf("effects %s slots %v; want bind without captures", effectString(e), e.Slots)
	}
}

// TestAnalyzeScan checks that a scan records the expectation where it stops and fails only
// below its minimum.
func TestAnalyzeScan(t *testing.T) {
	for _, min := range []int{0, 1} {
		s := &Scan{Class: &Class{Ranges: []Range{{'a', 'z'}}}, Min: min, Max: -1}
		analyzed(t, module(rule(0, "a", s)))
		want := EffConsume | EffExpect
		if min > 0 {
			want |= EffFail
		}
		if s.Attrs().Eff.Set != want || s.Attrs().Nullable != (min == 0) {
			t.Errorf("min %d: effects %s nullable=%v, want %s", min, effectString(s.Attrs().Eff), s.Attrs().Nullable, effectString(Effects{Set: want}))
		}
	}
}

func TestAnalyzeRaises(t *testing.T) {
	x := &SlotRef{Slot: 0, Name: "x"}
	for _, c := range []struct {
		term Term
		want bool
	}{
		{&Const{Value: 1}, false},
		{x, false},
		{&Convert{NodeType: "N", X: x, Proven: true}, false},
		{&Convert{NodeType: "N", X: x}, true},
		{&Convert{NodeType: "N", X: &Member{X: x, Name: "F"}, Proven: true}, true},
		{&Member{X: x, Name: "F"}, true},
	} {
		r := rule(0, "a", &Capture{Slot: 0, Name: "x", Body: lit("a")}, "x")
		r.Finish = Finish{Kind: FinishAction, Action: c.term}
		analyzed(t, module(r))
		if got := Raises(c.term); got != c.want {
			t.Errorf("%s: raises %v, want %v", TermString(c.term), got, c.want)
		}
		if got := r.Body.Attrs().Eff.Set&EffRaise != 0; got {
			t.Errorf("%s: the body raises", TermString(c.term))
		}
	}
}

// Calls of recursive rules can exceed the nesting limit, and a missing-token insertion the limit of
// insertions. A call of another rule and a Nest do not.
func TestAnalyzeLimitsRaise(t *testing.T) {
	call := &Call{Rule: 1}
	plain := &Call{Rule: 2}
	nest := &Nest{Rule: 2, Body: lit("c")}
	missing := &Missing{Lit: lit(";")}
	analyzed(t, module(rule(0, "a", seq(call, plain, nest, missing)), rule(1, "b", choice(lit("b"), &Call{Rule: 1})), rule(2, "c", lit("c"))))
	for _, c := range []struct {
		n     Node
		raise bool
	}{{call, true}, {plain, false}, {nest, false}, {missing, true}} {
		if got := c.n.Attrs().Eff.Set&EffRaise != 0; got != c.raise {
			t.Errorf("%T: effects %s, raise %v, want %v", c.n, effectString(c.n.Attrs().Eff), got, c.raise)
		}
	}
}

func TestAnalyzeRuleFixedPoint(t *testing.T) {
	// a = b "x" / "y";  b = a? ;  c = d;  d = "z"?
	m := module(
		rule(0, "a", choice(seq(&Call{Rule: 1}, lit("x")), lit("y"))),
		rule(1, "b", &Opt{Body: &Call{Rule: 0}}),
		rule(2, "c", &Call{Rule: 3}),
		rule(3, "d", &Opt{Body: lit("z")}),
	)
	m.Rules[0].Policy.Leader = true
	analyzed(t, m)
	for id, want := range []bool{false, true, true, true} {
		if got := m.Rules[id].Body.Attrs().Nullable; got != want {
			t.Errorf("rule %s: nullable=%v, want %v", m.Rules[id].Name, got, want)
		}
	}
	if call := m.Rules[2].Body.Attrs(); call.Eff.Set&EffFail != 0 || call.Eff.Set&(EffConsume|EffCall) != EffConsume|EffCall {
		t.Errorf("call d: effects %s", effectString(call.Eff))
	}
}

func TestAnalyzeRepeatMinimum(t *testing.T) {
	for _, c := range []struct {
		min  int
		want bool
	}{{0, true}, {1, true}, {2, false}} {
		r := &Repeat{Min: c.min, Max: -1, Body: &Opt{Body: lit("a")}}
		analyzed(t, module(rule(0, "r", r)))
		if r.Attrs().Nullable != c.want {
			t.Errorf("minimum %d: nullable=%v", c.min, r.Attrs().Nullable)
		}
	}
}

func TestAnalyzeFirst(t *testing.T) {
	// a = x:@("ab" c);  b = c;  c = -[0-9] "q";  l = l "x" / "y" (leader);  o = d;  d = l
	digit := class('0', '9')
	m := module(
		rule(0, "a", &Capture{Slot: 0, Name: "x", Body: &Text{Body: seq(lit("ab"), &Call{Rule: 2})}}, "x"),
		rule(1, "b", &Call{Rule: 2}),
		rule(2, "c", seq(&Drop{Body: digit}, lit("q"))),
		rule(3, "l", choice(seq(&Call{Rule: 3}, lit("x")), lit("y"))),
		rule(4, "o", &Call{Rule: 5}),
		rule(5, "d", &Call{Rule: 3}),
		rule(6, "n", &Nest{Rule: 1, Body: &Nest{Rule: 2, Body: seq(&Drop{Body: &ClassNode{Class: digit.Class}}, lit("q"))}}),
	)
	m.Rules[3].Policy.Leader = true
	m.Rules[2].Policy.Rec = true // a call of a recursive rule counts a level, a Nest and a call of another rule none
	analyzed(t, m)
	for _, c := range []struct {
		rule       int
		first, exp string
	}{
		{0, "'a'", `"ab"`},
		{1, "[0-9]+1", "[0-9]"},
		{2, "[0-9]", "[0-9]"},
		{6, "[0-9]", "[0-9]"},
	} {
		f := m.Rules[c.rule].Body.Attrs().First
		if f == nil || firstString(m, f) != c.first || expString(m, f.Exp) != c.exp {
			t.Errorf("rule %s: first %+v, want %s recording %s", m.Rules[c.rule].Name, f, c.first, c.exp)
		}
	}
	for _, id := range []int{3, 4, 5} {
		if f := m.Rules[id].Body.Attrs().First; f != nil {
			t.Errorf("rule %s: first %s, want none (behind a leader)", m.Rules[id].Name, firstString(m, f))
		}
	}
}

// A call met again after the first item of a rule that calls it gets the callee's First: the
// rule is not entered where its First is being computed.
func TestAnalyzeFirstThroughLaterItems(t *testing.T) {
	// p = "if" q;  q = p / "{"
	inner := &Call{Rule: 0}
	m := analyzed(t, module(
		rule(0, "p", seq(lit("if"), &Call{Rule: 1})),
		rule(1, "q", choice(inner, lit("{"))),
	))
	if f := inner.Attrs().First; f == nil || firstString(m, f) != "'i'+1" {
		t.Errorf("call p in q: first %+v, want 'i'+1", f)
	}
}

func TestCallGraph(t *testing.T) {
	// a = b c;  b = a? ;  c = Nest(d, "x");  d = d?;  e = "e"
	m := module(
		rule(0, "a", seq(&Call{Rule: 1}, &Call{Rule: 2})),
		rule(1, "b", &Opt{Body: &Call{Rule: 0}}),
		rule(2, "c", &Nest{Rule: 3, Body: lit("x")}),
		rule(3, "d", &Opt{Body: &Call{Rule: 3}}),
		rule(4, "e", lit("e")),
	)
	if want := [][]int{{1, 2}, {0}, {3}, {3}, nil}; !slices.EqualFunc(m.Calls, want, slices.Equal) {
		t.Errorf("calls %v, want %v", m.Calls, want)
	}
	if m.SCC[0] != m.SCC[1] || m.SCC[2] == m.SCC[0] || m.SCC[3] == m.SCC[2] || m.SCC[4] == m.SCC[0] {
		t.Errorf("components %v", m.SCC)
	}
	for id, want := range []bool{true, true, false, true, false} {
		if got := m.Cyclic[m.SCC[id]]; got != want {
			t.Errorf("rule %s: cyclic %v, want %v", m.Rules[id].Name, got, want)
		}
	}
	analyzed(t, m)
}

func TestVerifyRejects(t *testing.T) {
	cases := []struct {
		name string
		mod  func() *Module
		want string
	}{
		{"slot out of scope", func() *Module {
			return module(rule(0, "a", &Capture{Slot: 1, Name: "x", Body: lit("a")}, "x"))
		}, "slot 1 x"},
		{"slot name", func() *Module {
			return module(rule(0, "a", &Capture{Slot: 0, Name: "y", Body: lit("a")}, "x"))
		}, "slot 0 y"},
		{"term slot", func() *Module {
			r := rule(0, "a", lit("a"))
			r.Finish = Finish{Kind: FinishAction, Action: &SlotRef{Slot: 0, Name: "x"}}
			return module(r)
		}, "slot 0 x"},
		{"repeat scope", func() *Module {
			body := &Repeat{Max: -1, Scope: slots("y"), Body: &Capture{Slot: 0, Name: "x", Body: lit("a")}}
			return module(rule(0, "a", body, "x"))
		}, "slot 0 x"},
		{"capture in negative lookahead", func() *Module {
			return module(rule(0, "a", &Look{Neg: true, Body: &Capture{Slot: 0, Name: "x", Body: lit("a")}}, "x"))
		}, "inside negative lookahead"},
		{"capture in text", func() *Module {
			return module(rule(0, "a", &Text{Body: seq(lit("b"), &Capture{Slot: 0, Name: "x", Body: lit("a")})}, "x"))
		}, "inside text"},
		{"capture in recovery skip", func() *Module {
			return module(rule(0, "a", &Recover{Body: lit("b"), Skip: &Capture{Slot: 0, Name: "x", Body: lit("a")}}, "x"))
		}, "inside recovery skip"},
		{"value in text", func() *Module {
			body := lit("a")
			body.Attrs().Want = true
			return module(rule(0, "a", &Text{Body: body}))
		}, "the body of text wants a value"},
		{"value in drop", func() *Module {
			body := lit("a")
			body.Attrs().Want = true
			return module(rule(0, "a", &Drop{Body: body}))
		}, "the body of drop wants a value"},
		{"value in lookahead", func() *Module {
			body := lit("a")
			body.Attrs().Want = true
			return module(rule(0, "a", &Look{Body: body}))
		}, "lookahead without captures wants a value"},
		{"call", func() *Module {
			return module(rule(0, "a", &Call{Rule: 3}))
		}, "no rule 3"},
		{"nest cycle", func() *Module {
			return module(
				rule(0, "a", &Nest{Rule: 1, Body: &Nest{Rule: 0, Body: lit("a")}}),
				rule(1, "b", lit("b")),
			)
		}, "nest of a in a rule of its component"},
		{"nest cycle through a call", func() *Module {
			// a = b;  b = Nest(a, call b)
			return module(
				rule(0, "a", &Call{Rule: 1}),
				rule(1, "b", &Nest{Rule: 0, Body: &Call{Rule: 1}}),
			)
		}, "nest of a in a rule of its component"},
		{"nest inside a nest of its component", func() *Module {
			// c = d?;  d = c?;  x = Nest(c, Nest(d, "x"))
			return module(
				rule(0, "c", &Opt{Body: &Call{Rule: 1}}),
				rule(1, "d", &Opt{Body: &Call{Rule: 0}}),
				rule(2, "x", &Nest{Rule: 0, Body: &Nest{Rule: 1, Body: lit("x")}}),
			)
		}, "nest of d inside a nest of its component"},
		{"nest inside a nest of its rule", func() *Module {
			// a = "a";  x = Nest(a, Nest(a, "a"))
			return module(
				rule(0, "a", lit("a")),
				rule(1, "x", &Nest{Rule: 0, Body: &Nest{Rule: 0, Body: lit("a")}}),
			)
		}, "nest of a inside a nest of its component"},
		{"stale call graph", func() *Module {
			m := module(rule(0, "a", lit("a")), rule(1, "b", lit("b")))
			m.Rules[0].Body = &Call{Rule: 1}
			return m
		}, "calls [], body calls [1]"},
		{"stale components", func() *Module {
			m := module(rule(0, "a", &Call{Rule: 1}), rule(1, "b", &Opt{Body: &Call{Rule: 0}}))
			m.SCC[1]++
			return m
		}, "components"},
		{"operators rule inside", func() *Module {
			o := &OperatorsRule{Operands: []*OpLine{{Body: lit("a")}}}
			return module(rule(0, "a", seq(lit("b"), o)))
		}, "operators rule inside the body"},
		{"operator level", func() *Module {
			o := &OperatorsRule{Operands: []*OpLine{{Body: lit("a")}}, Ops: []*Operator{{ID: 0, Kind: "infix", Line: &OpLine{Body: lit("+")}}}}
			return module(rule(0, "a", o))
		}, "operator 0 at level 0"},
		{"guard", func() *Module {
			c := choice(lit("a"), lit("b"))
			c.Alts[1].Guard = &First{Lit: true, Rune: 'a'}
			return module(rule(0, "a", c))
		}, "not the first terminal"},
		{"guarded class", func() *Module {
			c := choice(lit("a"), lit("b"))
			m := module(rule(0, "a", c))
			c.Alts[1].Guard = &First{Lit: true, Rune: 'b', Exp: c.Alts[1].Body.(*Lit).Exp}
			c.Alts[1].GuardedClass = true
			return m
		}, "guarded class"},
		{"guard in a terminal", func() *Module {
			c := choice(lit("a"), lit("b"))
			m := module(rule(0, "a", &Terminal{Body: c}))
			c.Alts[1].Guard = &First{Lit: true, Rune: 'b', Exp: c.Alts[1].Body.(*Lit).Exp}
			return m
		}, "terminal: guarded alternative"},
		{"stream outside a start rule", func() *Module {
			return module(rule(0, "a", &Stream{Repeat: &Repeat{Max: -1, Body: lit("a")}}))
		}, "stream not at the top of a start rule"},
		{"stream below the top", func() *Module {
			m := module(rule(0, "a", seq(lit("b"), &Opt{Body: &Stream{Repeat: &Repeat{Max: -1, Body: lit("a")}}})))
			m.Starts = []int{0}
			return m
		}, "stream not at the top of a start rule"},
		{"stream below a label below the top", func() *Module {
			m := module(rule(0, "a", &Label{Body: seq(lit("b"), &Opt{Body: &Stream{Repeat: &Repeat{Max: -1, Body: lit("a")}}})}))
			m.Starts = []int{0}
			return m
		}, "stream not at the top of a start rule"},
		{"terminal", func() *Module {
			return module(rule(0, "a", &Terminal{Body: seq(lit("a"), &Any{})}))
		}, "terminal: Any"},
		{"unknown expectation", func() *Module {
			m := module(rule(0, "a", lit("a")))
			m.Rules[0].Body.(*Lit).Exp = 5
			return m
		}, "no expectation 5"},
		{"class not interned", func() *Module {
			m := module(rule(0, "a", class('a', 'z')))
			m.Rules[0].Body.(*ClassNode).Class = &Class{Ranges: []Range{{'a', 'z'}}}
			return m
		}, "is not the module's"},
		{"class interned twice", func() *Module {
			m := module(rule(0, "a", class('a', 'z')))
			m.Classes = append(m.Classes, m.Classes[0])
			return m
		}, "interned twice"},
		{"twin", func() *Module {
			r := rule(0, "a", lit("a"))
			r.Twin = rule(1, "a", lit("a"))
			return module(r, r.Twin)
		}, "not value-free"},
		{"untyped rule", func() *Module {
			m := module(rule(0, "a", lit("a")))
			m.Typed = true
			return m
		}, "rule a: no value type"},
		{"untyped slot", func() *Module {
			r := rule(0, "a", &Capture{Slot: 0, Name: "x", Body: lit("a")}, "x")
			r.Type = "T"
			m := module(r)
			m.Typed = true
			return m
		}, "slot x has no type"},
		{"rule ID", func() *Module {
			return module(rule(1, "a", lit("a")))
		}, "ID 1 at index 0"},
		{"rule without body", func() *Module {
			return module(rule(0, "a", nil))
		}, "rule a: no body"},
		{"start rule", func() *Module {
			m := module(rule(0, "a", lit("a")))
			m.Starts = []int{3}
			return m
		}, "start: no rule 3"},
		{"nest of a missing rule", func() *Module {
			return module(rule(0, "a", &Nest{Rule: 3, Body: lit("a")}))
		}, "no rule 3"},
		{"action finish without action", func() *Module {
			r := rule(0, "a", lit("a"))
			r.Finish = Finish{Kind: FinishAction}
			return module(r)
		}, "action finish without action"},
		{"call graph size", func() *Module {
			m := module(rule(0, "a", lit("a")))
			m.SCC = nil
			return m
		}, "call graph of 1 and 0 rules for 1 rules"},
		{"cyclic flags", func() *Module {
			m := module(rule(0, "a", &Opt{Body: &Call{Rule: 0}}))
			m.Cyclic[0] = false
			return m
		}, "cyclic [false]"},
		{"missing without literal", func() *Module {
			return module(rule(0, "a", &Missing{Lit: &Lit{}}))
		}, "missing without literal"},
		{"slot of an operators rule line", func() *Module {
			// The rule has the slot, but the line's own scope does not.
			o := &OperatorsRule{Operands: []*OpLine{{Body: &Capture{Slot: 0, Name: "x", Body: lit("a")}}}}
			return module(rule(0, "a", o, "x"))
		}, "slot 0 x not in scope []"},
		{"slot of a nest", func() *Module {
			// The enclosing rule has the slot x, but the Nest has the scope of rule a, which has y.
			return module(
				rule(0, "a", lit("a"), "y"),
				rule(1, "b", &Nest{Rule: 0, Body: &Capture{Slot: 0, Name: "x", Body: lit("a")}}, "x"),
			)
		}, "slot 0 x not in scope [y]"},
		{"stream in a capture of a capture", func() *Module {
			stream := &Stream{Repeat: &Repeat{Max: -1, Body: lit("a")}}
			inner := &Capture{Slot: 1, Name: "y", Body: stream}
			m := module(rule(0, "a", seq(&Capture{Slot: 0, Name: "x", Body: inner}), "x", "y"))
			m.Starts = []int{0}
			return m
		}, "stream not at the top of a start rule"},
		{"class expectation", func() *Module {
			m := module(rule(0, "a", class('a', 'z')))
			m.Classes[0].Exp = 99
			return m
		}, "class [a-z]: no expectation 99"},
		{"label expectation", func() *Module {
			m := module(rule(0, "a", &Label{Body: lit("a")}))
			m.Rules[0].Body.(*Label).Exp = 99
			return m
		}, "label: no expectation 99"},
		{"guard expectation", func() *Module {
			// The alternative's literal has the unknown expectation, and the guard equals its first
			// terminal, so only the expectation is wrong.
			c := choice(lit("a"), lit("b"))
			m := module(rule(0, "a", c))
			c.Alts[1].Body.(*Lit).Exp = 99
			c.Alts[1].Guard = &First{Lit: true, Rune: 'b', Exp: 99}
			return m
		}, "alternative 1: no expectation 99"},
		{"guarded class behind a call", func() *Module {
			// The alternative begins with a class only through a call, so a guard that consumed it
			// would not have read the class itself.
			c := choice(lit("a"), &Call{Rule: 1})
			m := module(rule(0, "a", c), rule(1, "b", class('x', 'y')))
			m.Rules[1].Policy.Rec = true
			cl := m.Rules[1].Body.(*ClassNode).Class
			c.Alts[1].Guard = &First{Class: cl, Exp: cl.Exp, Depth: 1}
			c.Alts[1].GuardedClass = true
			return m
		}, "guarded class: alternative does not begin with class"},
		{"repetition minimum", func() *Module {
			return module(rule(0, "a", &Repeat{Min: -1, Max: -1, Body: lit("a")}))
		}, "repetition bounds -1, -1"},
		{"repetition maximum", func() *Module {
			return module(rule(0, "a", &Repeat{Min: 2, Max: 1, Body: lit("a")}))
		}, "repetition bounds 2, 1"},
		{"scan minimum", func() *Module {
			return module(rule(0, "a", &Scan{Min: -1, Max: -1}))
		}, "scan bounds -1, -1"},
		{"scan maximum", func() *Module {
			return module(rule(0, "a", &Scan{Min: 2, Max: 1}))
		}, "scan bounds 2, 1"},
		{"scan class", func() *Module {
			m := module(rule(0, "a", &Scan{Class: class('a', 'z').Class, Min: 0, Max: -1}))
			m.Rules[0].Body.(*Scan).Class = &Class{Ranges: []Range{{'a', 'z'}}}
			return m
		}, "is not the module's"},
		{"scan wants a value", func() *Module {
			n := &Scan{Min: 0, Max: -1}
			n.Attrs().Want = true
			return module(rule(0, "a", n))
		}, "scan wants a value"},
		{"class run without classes", func() *Module {
			return module(rule(0, "a", &ClassRun{}))
		}, "class run of 0 classes"},
		{"class run wants a value", func() *Module {
			n := &ClassRun{Classes: []*Class{class('a', 'z').Class}}
			n.Attrs().Want = true
			return module(rule(0, "a", n))
		}, "class run of 1 classes, want true"},
		{"class run class", func() *Module {
			c := &ClassRun{Classes: []*Class{class('a', 'z').Class, class('0', '9').Class}}
			m := module(rule(0, "a", c))
			c.Classes[1] = &Class{Ranges: []Range{{'0', '9'}}}
			return m
		}, "is not the module's"},
		{"terminal wants a value", func() *Module {
			n := &Terminal{Body: lit("a")}
			n.Attrs().Want = true
			return module(rule(0, "a", n))
		}, "terminal wants a value"},
		{"value in negative lookahead", func() *Module {
			body := lit("a")
			body.Attrs().Want = true
			return module(rule(0, "a", &Look{Neg: true, Body: body}))
		}, "the body of negative lookahead wants a value"},
		{"value in recovery skip", func() *Module {
			skip := lit("a")
			skip.Attrs().Want = true
			return module(rule(0, "a", &Recover{Body: lit("b"), Skip: skip}))
		}, "the body of recovery skip wants a value"},
		{"capture in drop", func() *Module {
			return module(rule(0, "a", &Drop{Body: &Capture{Slot: 0, Name: "x", Body: lit("a")}}, "x"))
		}, "inside drop"},
		{"untyped field compare operand", func() *Module {
			l, r := &Const{Value: 1}, &Const{Value: 2}
			fc := &FieldCompare{Op: "==", L: Operand{X: l, Type: "int"}, R: Operand{X: r}}
			for _, t := range []Term{fc, l, r} {
				SetType(t, "int")
			}
			rl := rule(0, "a", lit("a"))
			rl.Type = "T"
			rl.Finish = Finish{Kind: FinishAction, Action: fc}
			m := module(rl)
			m.Typed = true
			return m
		}, "operand without type"},
		{"untyped term", func() *Module {
			r := rule(0, "a", lit("a"))
			r.Type = "T"
			r.Finish = Finish{Kind: FinishAction, Action: &Const{Value: 1}}
			m := module(r)
			m.Typed = true
			return m
		}, "term 1 has no type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.mod()
			// Analyze requires valid rule references, rule IDs and class runs.
			if !strings.Contains(c.want, "no rule") && !strings.Contains(c.want, "at index") && !strings.Contains(c.want, "class run of 0") {
				Analyze(m)
			}
			err := Verify(m)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Verify: %v, want an error containing %q", err, c.want)
			}
		})
	}
}

// TestVerifyAcceptsStreamInLabeledSequence checks that a stream in a sequence that a label wraps, or in
// a sequence nested in a label, is at the top of a start rule, as in the grammar.
func TestVerifyAcceptsStreamInLabeledSequence(t *testing.T) {
	stream := func() *Stream { return &Stream{Repeat: &Repeat{Max: -1, Body: lit("a")}} }
	for name, body := range map[string]Node{
		"label around the sequence": &Label{Body: seq(stream(), lit("!"))},
		"label around the stream":   seq(&Label{Body: stream()}, lit("!")),
		"nested labeled sequence":   seq(&Label{Body: seq(lit("b"), stream())}, lit("!")),
	} {
		m := module(rule(0, "a", body))
		m.Starts = []int{0}
		Analyze(m)
		if err := Verify(m); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVerifyAccepts(t *testing.T) {
	c := choice(seq(class('a', 'z'), lit("x")), lit("1"))
	stream := &Stream{Repeat: &Repeat{Max: -1, Body: &Call{Rule: 0}}}
	m := module(rule(0, "a", c), rule(1, "s", seq(lit("s"), &Capture{Slot: 0, Name: "x", Body: stream}), "x"))
	m.Starts = []int{1}
	Analyze(m)
	for _, a := range c.Alts {
		a.Guard = a.Body.Attrs().First
	}
	c.Alts[0].GuardedClass = true
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	// The same module, typed.
	m.Typed = true
	for _, r := range m.Rules {
		r.Type = "T"
		for i := range r.Scope {
			r.Scope[i].Type = "T"
		}
	}
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
}

func TestPrint(t *testing.T) {
	r := rule(0, "a", seq(&Capture{Slot: 0, Name: "x", Body: lit("a")}, &Opt{Body: &Cut{}}), "x")
	r.Finish = Finish{Kind: FinishAction, Action: &Builtin{Func: "text", Args: []Term{&SlotRef{Slot: 0, Name: "x"}}}}
	m := analyzed(t, module(r))
	got := Print(m, true)
	want := `rule 0 a want scope=[x] => text($0:x)
  seq [first='a' consume,fail,expect,capture[0]]
    capture 0 x [first='a' consume,fail,expect,capture[0]]
      lit "a" [first='a' consume,fail,expect]
    opt [nullable]
      cut [nullable cut]
`
	if got != want {
		t.Errorf("Print:\n%s\nwant:\n%s", got, want)
	}
}
