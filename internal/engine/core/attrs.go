package core

import "unicode/utf8"

// Analyze computes Nullable, First and Eff of every node of the module from the rules' bodies.
// Want, Shape and Steps are left as built. The rule references of the module must be valid.
//
// Nullable and the effects of calls are the least fixed point over the rules. First follows the
// first item of sequences, the bodies of captures, Text, Drop, Nest and Terminal, the first class
// of a class run and calls of rules that are neither left-recursion leaders nor Operators Rules,
// counting one level for each call and Nest on the way.
func Analyze(m *Module) {
	a := &analyzer{m: m, null: make([]bool, len(m.Rules)), eff: make([]Effect, len(m.Rules))}
	for changed := true; changed; {
		changed = false
		for _, r := range m.Rules {
			if r == nil {
				continue
			}
			n, e := a.node(r.Body)
			if r.Finish.Kind == FinishAction && Raises(r.Finish.Action) {
				e.Set |= EffRaise
			}
			s := e.Set &^ (EffCapture | EffBind | EffCut)
			if n != a.null[r.ID] || s != a.eff[r.ID] {
				a.null[r.ID], a.eff[r.ID] = n, s
				changed = true
			}
		}
	}
	a.first = make([]*First, len(m.Rules))
	a.state = make([]uint8, len(m.Rules))
	for _, r := range m.Rules {
		if r != nil {
			a.firsts(r.Body)
		}
	}
}

type analyzer struct {
	m     *Module
	null  []bool
	eff   []Effect
	first []*First
	// state is 0 for a rule whose First is not computed, 1 while it is computed, 2 after.
	state []uint8
}

// node sets the Nullable and Eff attributes of n and its subexpressions and returns them.
func (a *analyzer) node(n Node) (bool, Effects) {
	if n == nil {
		return true, Effects{}
	}
	var null bool
	var e Effects
	const local = EffCapture | EffBind | EffCut
	switch n := n.(type) {
	case *Lit:
		null = n.Text == ""
		if !null {
			e.Set = EffConsume | EffFail | EffExpect
		}
	case *ClassNode, *Any:
		e.Set = EffConsume | EffFail | EffExpect
	case *Anchor:
		null = true
		e.Set = EffFail | EffExpect
	case *Seq:
		null = true
		for _, it := range n.Items {
			nn, ee := a.node(it)
			null = null && nn
			e = union(e, ee)
		}
	case *Choice:
		for _, alt := range n.Alts {
			nn, ee := a.node(alt.Body)
			null = null || nn
			e = union(e, ee)
		}
		e.Set &^= EffCut
	case *Opt:
		_, ee := a.node(n.Body)
		null = true
		e = ee
		e.Set &^= EffCut | EffFail
	case *Repeat:
		nn, ee := a.node(n.Body)
		null = n.Min == 0 || n.Min == 1 && nn
		e = ee
		if n.Scope != nil {
			// The element's captures are its own; its bindings are the rule's, as everywhere
			// outside a rule call.
			e = Effects{Set: e.Set &^ EffCapture}
		}
		e.Set &^= EffCut
		if n.Min == 0 {
			e.Set &^= EffFail
		}
	case *Look:
		_, ee := a.node(n.Body)
		null = true
		e = ee
		e.Set &^= EffConsume | EffExpect | EffCut
		e.Set |= EffFail
	case *Cut:
		null = true
		e.Set = EffCut
	case *Succeed:
		null = true
	case *FailNode:
		e.Set = EffFail
	case *Call:
		// A call can exceed the nesting limit.
		null = a.null[n.Rule]
		e.Set = a.eff[n.Rule] | EffCall | EffRaise
	case *Nest:
		nn, ee := a.node(n.Body)
		null = nn
		e.Set = ee.Set&^local | EffCall | EffRaise
	case *OperatorsRule:
		for _, l := range n.Lines() {
			_, ee := a.node(l.Body)
			e.Set |= ee.Set &^ local
			if l.Action != nil && Raises(l.Action) {
				e.Set |= EffRaise
			}
		}
		for _, l := range n.Operands {
			null = null || l.Body.Attrs().Nullable
		}
		e.Set |= EffConsume | EffFail | EffExpect | EffCall | EffRaise
	case *Text:
		null, e = a.node(n.Body)
	case *Drop:
		null, e = a.node(n.Body)
	case *Capture:
		null, e = a.node(n.Body)
		e = union(Effects{Set: EffCapture, Slots: []int{n.Slot}}, e)
	case *Pred:
		null = true
		e.Set = EffFail
		if Raises(n.Term) {
			e.Set |= EffRaise
		}
	case *BindNode:
		null = true
		e.Set = EffBind
		if Raises(n.Term) {
			e.Set |= EffRaise
		}
	case *Label:
		null, e = a.node(n.Body)
	case *Recover:
		_, eb := a.node(n.Body)
		_, es := a.node(n.Skip)
		null = true
		e = union(eb, es)
		e.Set &^= EffFail
		e.Set |= EffRecover
	case *Missing:
		// An insertion can exceed the limit of missing tokens.
		a.node(n.Lit)
		null = true
		e.Set = EffConsume | EffExpect | EffRecover | EffRaise
	case *Stream:
		null, e = a.node(n.Repeat)
		e.Set |= EffCommit
	case *Scan:
		// A scan records the expectation of the character where it stops.
		null = n.Min == 0
		e.Set = EffConsume | EffExpect
		if n.Min > 0 {
			e.Set |= EffFail
		}
	case *ClassRun:
		e.Set = EffConsume | EffFail | EffExpect
	case *Terminal:
		null, e = a.node(n.Body)
	}
	at := n.Attrs()
	at.Nullable, at.Eff = null, e
	return null, e
}

// union returns the union of two effect summaries, with the slots of x first.
func union(x, y Effects) Effects {
	s := Effects{Set: x.Set | y.Set, Slots: x.Slots}
	for _, v := range y.Slots {
		if !hasSlot(s.Slots, v) {
			s.Slots = append(s.Slots[:len(s.Slots):len(s.Slots)], v)
		}
	}
	return s
}

func hasSlot(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// firsts sets the First attribute of n and its subexpressions.
func (a *analyzer) firsts(n Node) {
	if n == nil {
		return
	}
	if o, ok := n.(*OperatorsRule); ok {
		for _, l := range o.Lines() {
			a.firsts(l.Body)
		}
	}
	for _, c := range Children(n) {
		a.firsts(c)
	}
	n.Attrs().First = a.head(n)
}

// head returns the First of n, following only the expressions n must begin with, so that a rule
// is entered only where its call can be the first thing a body matches.
func (a *analyzer) head(n Node) *First {
	switch n := n.(type) {
	case *Lit:
		if n.Text != "" {
			r, _ := utf8.DecodeRuneInString(n.Text)
			return &First{Lit: true, Rune: r, Exp: n.Exp}
		}
	case *ClassNode:
		return &First{Class: n.Class, Exp: n.Class.Exp}
	case *Seq:
		if len(n.Items) > 0 {
			return a.head(n.Items[0])
		}
	case *Capture:
		return a.head(n.Body)
	case *Text:
		return a.head(n.Body)
	case *Drop:
		return a.head(n.Body)
	case *Call:
		return deeper(a.ruleFirst(n.Rule))
	case *Nest:
		return deeper(a.head(n.Body))
	case *Terminal:
		return a.head(n.Body)
	case *ClassRun:
		return &First{Class: n.Classes[0], Exp: n.Classes[0].Exp}
	}
	return nil
}

// ruleFirst returns the First of a rule's body, nil for a leader, an Operators Rule or a rule
// whose First is being computed.
func (a *analyzer) ruleFirst(id int) *First {
	r := a.m.Rules[id]
	if r.Policy.Leader {
		return nil
	}
	if _, ok := r.Body.(*OperatorsRule); ok {
		return nil
	}
	switch a.state[id] {
	case 1:
		return nil
	case 2:
		return a.first[id]
	}
	a.state[id] = 1
	a.first[id] = a.head(r.Body)
	a.state[id] = 2
	return a.first[id]
}

func deeper(f *First) *First {
	if f == nil {
		return nil
	}
	g := *f
	g.Depth++
	return &g
}
