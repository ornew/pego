package core

import (
	"fmt"
	"slices"
	"strings"
)

// Verify checks the invariants of an analyzed module and returns the first violation found:
//
//   - every rule has its index as ID and a body; start rules, calls and Nests name rules; a
//     twin is value-free;
//   - the call graph (Calls, SCC, Cyclic) is that of the rules' bodies, and a Nest of rule r
//     occurs only in rules outside r's component of the call graph and only inside Nests of
//     rules outside it;
//   - an OperatorsRule is the whole body of its rule; a Stream is at the top of a start rule:
//     its body, an item of its body sequence or the body of a capture that is one of these, or
//     the body of labels at one of these places;
//   - every Capture and SlotRef names a slot of its enclosing scope, by index and name;
//   - the bodies of negative lookaheads, Text, Drop and the skip expressions of Recover write no
//     capture of the enclosing scope and want no value; the body of a positive lookahead wants a
//     value only where it captures;
//   - expectations are those of the module, and classes are the module's (each one once);
//   - a choice guard is the First of its alternative, and an alternative whose guard is marked
//     GuardedClass begins with the guard's class; Terminal bodies have no guards;
//   - repetition and Scan bounds are ordered;
//   - a Terminal holds only literals, classes, sequences, choices, options, negative lookaheads,
//     Text and Drop, and wants no value; Scan and ClassRun want no value;
//   - in a typed module, every term but a lambda, every FieldCompare operand, every slot and the
//     value of every rule that is not value-free have a checked type.
func Verify(m *Module) error {
	v := &verifier{m: m, classes: map[*Class]bool{}}
	for _, c := range m.Classes {
		if v.classes[c] {
			return fmt.Errorf("class %s interned twice", classString(c))
		}
		v.classes[c] = true
		if err := v.exp(c.Exp); err != nil {
			return fmt.Errorf("class %s: %w", classString(c), err)
		}
	}
	for i, r := range m.Rules {
		if r == nil {
			continue
		}
		if r.ID != i {
			return fmt.Errorf("rule %s: ID %d at index %d", r.Name, r.ID, i)
		}
		if r.Body == nil {
			return fmt.Errorf("rule %s: no body", r.Name)
		}
		if r.Twin != nil && (!r.Twin.Policy.NoValue || r.Twin.Want) {
			return fmt.Errorf("rule %s: twin %s is not value-free", r.Name, r.Twin.Name)
		}
		if r.Finish.Kind == FinishAction && r.Finish.Action == nil {
			return fmt.Errorf("rule %s: action finish without action", r.Name)
		}
		if m.Typed && r.Type == nil && !r.Policy.NoValue {
			return fmt.Errorf("rule %s: no value type", r.Name)
		}
	}
	start := map[int]bool{}
	for _, s := range m.Starts {
		if err := v.ruleRef(s); err != nil {
			return fmt.Errorf("start: %w", err)
		}
		start[s] = true
	}
	if err := v.callGraph(); err != nil {
		return err
	}
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		v.rule, v.start = r, start[r.ID]
		v.top = topNodes(r.Body)
		if err := v.scope(r.Scope); err != nil {
			return fmt.Errorf("rule %s: %w", r.Name, err)
		}
		if err := v.term(r.Finish.Action, r.Scope); err != nil {
			return fmt.Errorf("rule %s: %w", r.Name, err)
		}
		if err := v.node(r.Body, r.Scope); err != nil {
			return fmt.Errorf("rule %s: %w", r.Name, err)
		}
	}
	return nil
}

type verifier struct {
	m       *Module
	classes map[*Class]bool
	rule    *Rule
	start   bool
	// top holds the nodes of the rule's body where a Stream may occur.
	top map[Node]bool
	// nests holds the components of the rules of the Nests around the node being checked.
	nests []int
}

func (v *verifier) ruleRef(id int) error {
	if id < 0 || id >= len(v.m.Rules) || v.m.Rules[id] == nil {
		return fmt.Errorf("no rule %d", id)
	}
	return nil
}

func (v *verifier) exp(id ExpID) error {
	if id < 0 || int(id) >= len(v.m.Exps) {
		return fmt.Errorf("no expectation %d", id)
	}
	return nil
}

func (v *verifier) class(c *Class) error {
	if !v.classes[c] {
		return fmt.Errorf("class %s is not the module's", classString(c))
	}
	return nil
}

// callGraph checks Calls, SCC and Cyclic against the rules' bodies.
func (v *verifier) callGraph() error {
	m := v.m
	if len(m.Calls) != len(m.Rules) || len(m.SCC) != len(m.Rules) {
		return fmt.Errorf("call graph of %d and %d rules for %d rules", len(m.Calls), len(m.SCC), len(m.Rules))
	}
	for i, r := range m.Rules {
		if r == nil {
			continue
		}
		if want := calls(r.Body); !slices.Equal(m.Calls[i], want) {
			return fmt.Errorf("rule %s: calls %v, body calls %v", r.Name, m.Calls[i], want)
		}
	}
	scc, cyclic := components(m.Calls)
	if !slices.Equal(m.SCC, scc) || !slices.Equal(m.Cyclic, cyclic) {
		return fmt.Errorf("components %v cyclic %v, want %v cyclic %v", m.SCC, m.Cyclic, scc, cyclic)
	}
	return nil
}

// topNodes returns the nodes of a rule body where a Stream may occur.
func topNodes(body Node) map[Node]bool {
	top := map[Node]bool{}
	items := []Node{body}
	if s, ok := body.(*Seq); ok {
		items = append(items, s.Items...)
	}
	mark := func(n Node) {
		for {
			top[n] = true
			l, ok := n.(*Label)
			if !ok {
				return
			}
			n = l.Body
		}
	}
	for _, it := range items {
		mark(it)
		if c, ok := it.(*Capture); ok {
			mark(c.Body)
		}
	}
	return top
}

func slotErr(scope []Slot, slot int, name string) error {
	if slot < 0 || slot >= len(scope) || scope[slot].Name != name {
		return fmt.Errorf("slot %d %s not in scope %v", slot, name, SlotNames(scope))
	}
	return nil
}

func (v *verifier) scope(scope []Slot) error {
	if !v.m.Typed {
		return nil
	}
	for _, s := range scope {
		if s.Type == nil {
			return fmt.Errorf("slot %s has no type", s.Name)
		}
	}
	return nil
}

func (v *verifier) term(t Term, scope []Slot) error {
	var err error
	WalkTerm(t, func(x Term) {
		if err != nil {
			return
		}
		if s, ok := x.(*SlotRef); ok {
			err = slotErr(scope, s.Slot, s.Name)
		}
		if _, lambda := x.(*Lambda); v.m.Typed && !lambda && TypeOf(x) == nil {
			err = fmt.Errorf("term %s has no type", TermString(x))
		}
		if c, ok := x.(*FieldCompare); ok && v.m.Typed && (c.L.Type == nil || c.R.Type == nil) {
			err = fmt.Errorf("term %s: operand without type", TermString(x))
		}
	})
	return err
}

func (v *verifier) node(n Node, scope []Slot) error {
	if n == nil {
		return fmt.Errorf("nil node")
	}
	switch n := n.(type) {
	case *Lit:
		if n.Text != "" {
			if err := v.exp(n.Exp); err != nil {
				return fmt.Errorf("literal %q: %w", n.Text, err)
			}
		}
	case *ClassNode:
		if err := v.class(n.Class); err != nil {
			return err
		}
	case *Choice:
		for i, a := range n.Alts {
			if err := v.guard(a); err != nil {
				return fmt.Errorf("alternative %d: %w", i, err)
			}
		}
	case *Repeat:
		if n.Min < 0 || n.Max >= 0 && n.Max < n.Min {
			return fmt.Errorf("repetition bounds %d, %d", n.Min, n.Max)
		}
		if n.Scope != nil {
			if err := v.scope(n.Scope); err != nil {
				return err
			}
			return v.node(n.Body, n.Scope)
		}
	case *Look:
		if n.Neg {
			if err := v.valueFree(n.Body, "negative lookahead"); err != nil {
				return err
			}
		} else if n.Body.Attrs().Want && n.Body.Attrs().Eff.Set&EffCapture == 0 {
			return fmt.Errorf("lookahead without captures wants a value")
		}
	case *Call:
		return v.ruleRef(n.Rule)
	case *Nest:
		if err := v.ruleRef(n.Rule); err != nil {
			return err
		}
		if v.m.SCC[n.Rule] == v.m.SCC[v.rule.ID] {
			return fmt.Errorf("nest of %s in a rule of its component of the call graph", v.m.Rules[n.Rule].Name)
		}
		if slices.Contains(v.nests, v.m.SCC[n.Rule]) {
			return fmt.Errorf("nest of %s inside a nest of its component of the call graph", v.m.Rules[n.Rule].Name)
		}
		v.nests = append(v.nests, v.m.SCC[n.Rule])
		err := v.node(n.Body, v.m.Rules[n.Rule].Scope)
		v.nests = v.nests[:len(v.nests)-1]
		return err
	case *OperatorsRule:
		if n != v.rule.Body {
			return fmt.Errorf("operators rule inside the body")
		}
		for _, l := range n.Lines() {
			if err := v.scope(l.Scope); err != nil {
				return err
			}
			if err := v.node(l.Body, l.Scope); err != nil {
				return err
			}
			if err := v.term(l.Action, l.Scope); err != nil {
				return err
			}
		}
		for _, o := range n.Ops {
			if o.Level < 1 {
				return fmt.Errorf("operator %d at level %d", o.ID, o.Level)
			}
		}
		return nil
	case *Text:
		if err := v.valueFree(n.Body, "text"); err != nil {
			return err
		}
	case *Drop:
		if err := v.valueFree(n.Body, "drop"); err != nil {
			return err
		}
	case *Capture:
		if err := slotErr(scope, n.Slot, n.Name); err != nil {
			return err
		}
	case *Pred:
		if err := v.term(n.Term, scope); err != nil {
			return err
		}
	case *BindNode:
		if err := v.term(n.Term, scope); err != nil {
			return err
		}
	case *Label:
		if err := v.exp(n.Exp); err != nil {
			return fmt.Errorf("label: %w", err)
		}
	case *Recover:
		if err := v.valueFree(n.Skip, "recovery skip"); err != nil {
			return err
		}
	case *Missing:
		if n.Lit == nil || n.Lit.Text == "" {
			return fmt.Errorf("missing without literal")
		}
	case *Stream:
		if !v.start || !v.top[n] {
			return fmt.Errorf("stream not at the top of a start rule")
		}
	case *Scan:
		if n.Min < 0 || n.Max >= 0 && n.Max < n.Min {
			return fmt.Errorf("scan bounds %d, %d", n.Min, n.Max)
		}
		if n.Class != nil {
			if err := v.class(n.Class); err != nil {
				return err
			}
		}
		if n.Attrs().Want {
			return fmt.Errorf("scan wants a value")
		}
	case *ClassRun:
		if len(n.Classes) == 0 || n.Attrs().Want {
			return fmt.Errorf("class run of %d classes, want %v", len(n.Classes), n.Attrs().Want)
		}
		for _, c := range n.Classes {
			if err := v.class(c); err != nil {
				return err
			}
		}
	case *Terminal:
		if n.Attrs().Want {
			return fmt.Errorf("terminal wants a value")
		}
		if err := terminalBody(n.Body); err != nil {
			return err
		}
	}
	for _, c := range Children(n) {
		if err := v.node(c, scope); err != nil {
			return err
		}
	}
	return nil
}

// valueFree reports an error if n wants a value or writes a capture of its enclosing scope.
func (v *verifier) valueFree(n Node, where string) error {
	if n.Attrs().Want {
		return fmt.Errorf("the body of %s wants a value", where)
	}
	return noCapture(n, where)
}

// guard checks the guard of an alternative against the First of its body.
func (v *verifier) guard(a *Alt) error {
	if a.Guard == nil {
		if a.GuardedClass {
			return fmt.Errorf("guarded class without guard")
		}
		return nil
	}
	if f := a.Body.Attrs().First; f == nil || *f != *a.Guard {
		return fmt.Errorf("guard %s is not the first terminal of the alternative", v.firstString(a.Guard))
	}
	if !a.Guard.Lit {
		if err := v.class(a.Guard.Class); err != nil {
			return err
		}
	}
	if err := v.exp(a.Guard.Exp); err != nil {
		return err
	}
	if a.GuardedClass {
		c, ok := a.Body.(*ClassNode)
		if s, isSeq := a.Body.(*Seq); isSeq && len(s.Items) > 0 {
			c, ok = s.Items[0].(*ClassNode)
		}
		if !ok || a.Guard.Depth != 0 || a.Guard.Lit || c.Class != a.Guard.Class {
			return fmt.Errorf("guarded class: alternative does not begin with class %s", v.firstString(a.Guard))
		}
	}
	return nil
}

func (v *verifier) firstString(f *First) string { return firstString(v.m, f) }

// noCapture reports an error if n writes a capture of its enclosing scope.
func noCapture(n Node, where string) error {
	var found *Capture
	var walk func(Node)
	walk = func(n Node) {
		if n == nil || found != nil {
			return
		}
		switch n := n.(type) {
		case *Capture:
			found = n
			return
		case *Repeat:
			if n.Scope != nil {
				return
			}
		case *Nest, *OperatorsRule:
			return
		}
		for _, c := range Children(n) {
			walk(c)
		}
	}
	walk(n)
	if found != nil {
		return fmt.Errorf("capture %s inside %s", found.Name, where)
	}
	return nil
}

// terminalBody reports an error if n is not a terminal region: its forms, without guards.
func terminalBody(n Node) error {
	switch n := n.(type) {
	case *Lit, *ClassNode:
		return nil
	case *Look:
		if !n.Neg {
			return fmt.Errorf("terminal: positive lookahead")
		}
	case *Choice:
		for _, a := range n.Alts {
			if a.Guard != nil {
				return fmt.Errorf("terminal: guarded alternative")
			}
		}
	case *Seq, *Opt, *Text, *Drop:
	default:
		return fmt.Errorf("terminal: %s", strings.TrimPrefix(fmt.Sprintf("%T", n), "*core."))
	}
	for _, c := range Children(n) {
		if err := terminalBody(c); err != nil {
			return err
		}
	}
	return nil
}
