package core

import "strings"

// TermOptions selects the term rewrites of RewriteTerms that a target may not want.
type TermOptions struct {
	// TextDispatch rewrites a predicate that is a disjunction of comparisons of the text of one
	// capture with literals to TextIn.
	TextDispatch bool
	// FieldCompare rewrites comparisons of a field read to FieldCompare.
	FieldCompare bool
}

// RewriteTerms rewrites the terms of the module into forms a target evaluates without
// intermediate values, each with the same result and the same errors in the same order:
//
//   - text(a) == text(b) and text(a) != text(b) become TextEq;
//   - with FieldCompare, a comparison with a field read (x.F, not a member $m) on either side
//     becomes FieldCompare;
//   - with TextDispatch, a predicate that is a disjunction of two or more comparisons
//     text(x) == "literal" (either way round) of the same capture or local x becomes TextIn;
//   - a concat call whose arguments are all list calls, map calls of two arguments or such
//     concat calls becomes ConcatFused;
//   - a map call over a projected repetition (Builtin.Projected) becomes ProjectedMap;
//   - New records whether its fields are given in declaration order (InOrder).
func RewriteTerms(m *Module, opts TermOptions) {
	w := &termRewriter{opts: opts}
	for _, r := range m.Rules {
		if r == nil {
			continue
		}
		if r.Finish.Action != nil {
			r.Finish.Action = w.term(r.Finish.Action)
		}
		Walk(r.Body, func(n Node) {
			switch n := n.(type) {
			case *Pred:
				n.Term = w.predicate(n.Term)
			case *BindNode:
				n.Term = w.term(n.Term)
			case *OperatorsRule:
				for _, l := range n.Lines() {
					if l.Action != nil {
						l.Action = w.term(l.Action)
					}
				}
			}
		})
	}
}

type termRewriter struct{ opts TermOptions }

func (w *termRewriter) predicate(t Term) Term {
	if w.opts.TextDispatch {
		if x, texts, ok := textMembership(t); ok {
			in := &TextIn{X: x, Texts: texts, Orig: w.term(t)}
			in.t = TypeOf(t)
			return in
		}
	}
	return w.term(t)
}

func (w *termRewriter) term(t Term) Term {
	switch t := t.(type) {
	case *Member:
		t.X = w.term(t.X)
	case *New:
		for i := range t.Fields {
			t.Fields[i].Value = w.term(t.Fields[i].Value)
		}
		t.InOrder = fieldsInOrder(t)
	case *Convert:
		t.X = w.term(t.X)
	case *Tuple:
		for i, x := range t.Elems {
			t.Elems[i] = w.term(x)
		}
	case *Builtin:
		if t.Func == "concat" && fusable(t) {
			c := &ConcatFused{}
			c.t = t.t
			w.concatParts(c, t)
			return c
		}
		if t.Projected {
			p := &ProjectedMap{X: w.term(t.Args[0])}
			p.t = t.t
			return p
		}
		for i, a := range t.Args {
			t.Args[i] = w.term(a)
		}
	case *Lambda:
		t.Body = w.term(t.Body)
	case *Binary:
		if t.Op == "==" || t.Op == "!=" {
			if lx, ok := textArg(t.L); ok {
				if rx, ok := textArg(t.R); ok {
					eq := &TextEq{L: w.term(lx), R: w.term(rx), Neg: t.Op == "!="}
					eq.t = t.t
					return eq
				}
			}
		}
		if w.opts.FieldCompare && comparison(t.Op) && (fieldRead(t.L) || fieldRead(t.R)) {
			c := &FieldCompare{Op: t.Op, L: w.operand(t.L), R: w.operand(t.R)}
			c.t = t.t
			return c
		}
		t.L, t.R = w.term(t.L), w.term(t.R)
	case *Unary:
		t.X = w.term(t.X)
	case *Cast:
		t.X = w.term(t.X)
	}
	return t
}

func (w *termRewriter) operand(t Term) Operand {
	if fieldRead(t) {
		m := t.(*Member)
		return Operand{X: w.term(m.X), Field: true, Name: m.Name, Type: TypeOf(m)}
	}
	return Operand{X: w.term(t), Type: TypeOf(t)}
}

// concatParts appends the parts of the fusable concat call t to c, flattening nested concat calls.
func (w *termRewriter) concatParts(c *ConcatFused, t *Builtin) {
	for _, a := range t.Args {
		b := a.(*Builtin) // fusable
		switch {
		case b.Func == "list":
			p := ConcatPart{Kind: PartList}
			for _, x := range b.Args {
				p.Args = append(p.Args, w.term(x))
			}
			c.Parts = append(c.Parts, p)
		case b.Func == "map" && b.Projected:
			c.Parts = append(c.Parts, ConcatPart{Kind: PartItems, X: w.term(b.Args[0])})
		case b.Func == "map":
			c.Parts = append(c.Parts, ConcatPart{Kind: PartMap, X: w.term(b.Args[0]), Fn: w.term(b.Args[1]).(*Lambda)})
		case b.Func == "concat":
			w.concatParts(c, b)
		}
	}
}

// fusable reports whether every argument of the concat call t is a list call, a map call of a
// value and a lambda, or a fusable concat call. The checker accepts only a lambda as the
// function of map, so the lambda test rejects no compiled map call.
func fusable(t *Builtin) bool {
	for _, a := range t.Args {
		b, ok := a.(*Builtin)
		switch {
		case ok && b.Func == "list":
		case ok && b.Func == "map" && len(b.Args) == 2:
			if _, lambda := b.Args[1].(*Lambda); !lambda {
				return false
			}
		case ok && b.Func == "concat" && fusable(b):
		default:
			return false
		}
	}
	return true
}

// textArg returns x if t is text(x).
func textArg(t Term) (Term, bool) {
	if b, ok := t.(*Builtin); ok && b.Func == "text" && len(b.Args) == 1 {
		return b.Args[0], true
	}
	return nil, false
}

func comparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=":
		return true
	}
	return false
}

// fieldRead reports whether t reads a field of a value, x.F.
func fieldRead(t Term) bool {
	m, ok := t.(*Member)
	return ok && !strings.HasPrefix(m.Name, "$")
}

// fieldsInOrder reports whether the fields given to t are declared fields, given in declaration
// order.
func fieldsInOrder(t *New) bool {
	next := 0
	for _, given := range t.Fields {
		for next < len(t.Declared) && t.Declared[next] != given.Name {
			next++
		}
		if next == len(t.Declared) {
			return false
		}
		next++
	}
	return true
}

// textMembership recognizes a disjunction of two or more comparisons text(x) == "literal", either
// way round, of the same capture or local x, and returns x and the literals without repeats, in
// order. Evaluating the text of x once then gives the result of the comparisons in order, which
// have no other effect.
func textMembership(t Term) (Term, []string, bool) {
	var x Term
	var texts []string
	var walk func(Term) bool
	walk = func(t Term) bool {
		b, ok := t.(*Binary)
		if ok && b.Op == "||" {
			return walk(b.L) && walk(b.R)
		}
		if !ok || b.Op != "==" {
			return false
		}
		y, lit := textOperand(b.L), stringConst(b.R)
		if y == nil || lit == nil {
			y, lit = textOperand(b.R), stringConst(b.L)
		}
		if y == nil || lit == nil || x != nil && !sameRef(x, y) {
			return false
		}
		x = y
		texts = append(texts, *lit)
		return true
	}
	if !walk(t) || len(texts) < 2 {
		return nil, nil, false
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range texts {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return x, out, true
}

// textOperand returns x if t is text(x) of a capture or a local.
func textOperand(t Term) Term {
	x, ok := textArg(t)
	if !ok {
		return nil
	}
	switch x.(type) {
	case *SlotRef, *Local:
		return x
	}
	return nil
}

func stringConst(t Term) *string {
	if c, ok := t.(*Const); ok {
		if s, ok := c.Value.(string); ok {
			return &s
		}
	}
	return nil
}

// sameRef reports whether two captures or locals name the same value.
func sameRef(x, y Term) bool {
	switch x := x.(type) {
	case *SlotRef:
		y, ok := y.(*SlotRef)
		return ok && x.Name == y.Name
	case *Local:
		y, ok := y.(*Local)
		return ok && x.Kind == y.Kind && x.Name == y.Name
	}
	return false
}
