package engine

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
)

// This file infers the types of rule values in the value model of Record 060, in which a rule
// produces an ordinary typed value and the syntax tree is built from one generic node[T, C]. The
// engine still builds the reserved node kinds; these types are computed beside the current ones so
// that the new model can be checked against every grammar before the backends move to it.

// Types of the value model, beside basicTy, namedTy, listTy and optTy.
const (
	tyTop    basicTy = "_"   // every value
	tyBottom basicTy = "_|_" // no value
	tyErr    basicTy = "error"
)

// tupleTy is a tuple (T, U, ...).
type tupleTy struct{ elems []ty }

func (t tupleTy) String() string {
	parts := make([]string, len(t.elems))
	for i, e := range t.elems {
		parts[i] = e.String()
	}
	if len(parts) == 1 {
		return "(" + parts[0] + ",)"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// capIndex locates a capture in the value of a node. In a path, the first index is an element of
// the node's tuple, and each further index an element of the tuple in the node at the previous
// one. A capture in choice alternatives of different shapes has a path for each shape; the
// alternative that matched selects one at run time.
type capIndex struct {
	name  string
	paths [][]int
	t     ty
}

// nodeTy is node[T, C]: a node whose Value has type value, with the captures caps.
type nodeTy struct {
	value ty
	caps  []capIndex // sorted by name
}

func (t nodeTy) String() string {
	if t.value == tyTop && len(t.caps) == 0 {
		return "node"
	}
	if len(t.caps) == 0 {
		return "node[" + t.value.String() + "]"
	}
	parts := make([]string, len(t.caps))
	for i, c := range t.caps {
		paths := make([]string, len(c.paths))
		for j, path := range c.paths {
			idx := make([]string, len(path))
			for k, p := range path {
				idx[k] = strconv.Itoa(p)
			}
			paths[j] = strings.Join(idx, ".")
		}
		parts[i] = strconv.Quote(c.name) + ": " + strings.Join(paths, " | ")
	}
	return "node[" + t.value.String() + ", {" + strings.Join(parts, ", ") + "}]"
}

// ruleTy is the value type of a rule, referred to by the rule's name.
type ruleTy struct{ name string }

func (t ruleTy) String() string { return t.name }

// vunion is the join of types in the value model: nested unions are flattened, duplicates and
// _|_ removed, and a union with _ is _. Since T <= *T, a union with an optional argument is the
// optional of the union of the arguments without their *.
func vunion(ts ...ty) ty {
	var alts []ty
	seen := map[string]bool{}
	optional := false
	var add func(t ty)
	add = func(t ty) {
		switch u := t.(type) {
		case unionTy:
			for _, a := range u.alts {
				add(a)
			}
			return
		case optTy:
			optional = true
			add(u.elem)
			return
		}
		// Rule and type names are separate namespaces, so the key includes the kind of type.
		key := fmt.Sprintf("%T %s", t, t)
		if t == nil || t == tyBottom || seen[key] {
			return
		}
		seen[key] = true
		alts = append(alts, t)
	}
	for _, t := range ts {
		add(t)
	}
	for _, a := range alts {
		if a == tyTop {
			return tyTop
		}
	}
	if optional {
		if len(alts) == 0 {
			return tyBottom
		}
		return optTy{vunion(alts...)}
	}
	switch len(alts) {
	case 0:
		return tyBottom
	case 1:
		return alts[0]
	}
	return unionTy{alts}
}

// vopt is *t; an optional value of an optional type is not nested, and *_|_ is _|_.
func vopt(t ty) ty {
	if t == tyTop || t == tyBottom {
		return t
	}
	return vunion(optTy{t})
}

var tyStringNode = nodeTy{value: tyString}

// valueElem is an element of the tuple an expression contributes to its scope.
type valueElem struct {
	t    ty
	caps []capIndex // the captures in this element, with paths relative to it
}

// valueInference computes value types. legacy holds the rule types of the current model, used for
// actions until actions are checked in the value model.
type valueInference struct {
	legacy  map[string]ty
	control *compileControl
}

// valueTypes returns the value type of each rule of a type-checked program. It is computed on
// demand: nothing uses it for compilation yet.
func (p *Program) valueTypes() map[string]ty {
	if p.typed == nil {
		return nil
	}
	p.typed.valuesOnce.Do(func() {
		p.typed.values = valueTypes(p.Grammar.Rules(), p.typed.rules, nil)
	})
	return p.typed.values
}

// valueTypes returns the value type of each rule.
func valueTypes(rules []*grammar.RuleDef, legacy map[string]ty, control *compileControl) map[string]ty {
	v := &valueInference{legacy: legacy, control: control}
	out := map[string]ty{}
	for _, rd := range rules {
		control.poll()
		out[rd.Name] = v.rule(rd)
	}
	return out
}

func (v *valueInference) rule(rd *grammar.RuleDef) ty {
	if rd.Action != nil {
		return fromLegacy(v.legacy[rd.Name])
	}
	if pr, ok := rd.Expr.(*grammar.OperatorsRule); ok {
		return v.operatorsRule(rd, pr)
	}
	return v.scopeValue(rd.Expr)
}

// scopeValue is the value of e as the body of a scope: a rule body, an operand or a repetition
// element. A scope whose body is a sequence, or that has captures, is a node of its
// tuple; otherwise its one element is its value, and a scope without elements is node[()].
func (v *valueInference) scopeValue(e grammar.Expr) ty {
	elems := v.elems(e)
	_, seq := ungroupExpr(e).(*grammar.Seq)
	if !seq && len(elems) == 1 && len(elems[0].caps) == 0 {
		return elems[0].t
	}
	return tupleNode(elems)
}

// tupleNode is the node of the tuple of elems, with their captures.
func tupleNode(elems []valueElem) nodeTy {
	n := nodeTy{value: tupleTy{elems: []ty{}}}
	var ts []ty
	for i, el := range elems {
		ts = append(ts, el.t)
		for _, c := range el.caps {
			paths := make([][]int, len(c.paths))
			for j, path := range c.paths {
				paths[j] = append([]int{i}, path...)
			}
			n.caps = append(n.caps, capIndex{c.name, paths, c.t})
		}
	}
	if len(ts) > 0 {
		n.value = tupleTy{ts}
	}
	sort.SliceStable(n.caps, func(i, j int) bool { return n.caps[i].name < n.caps[j].name })
	return n
}

// single is the value of e where it takes one element: an alternative, the operand of ? or a
// capture. Several elements become the node of their tuple. ok is false when e has no value.
// The captures of the result may be changed without changing its type.
func (v *valueInference) single(e grammar.Expr) (valueElem, bool) {
	return oneElem(v.elems(e), ungroupExpr(e))
}

func oneElem(elems []valueElem, e grammar.Expr) (valueElem, bool) {
	_, seq := e.(*grammar.Seq)
	switch {
	case len(elems) == 0:
		return valueElem{}, false
	case len(elems) == 1 && !seq:
		return elems[0], true
	}
	n := tupleNode(elems)
	return valueElem{t: n, caps: slices.Clone(n.caps)}, true
}

// elems returns the elements e contributes to the tuple of its scope.
func (v *valueInference) elems(e grammar.Expr) []valueElem {
	v.control.poll()
	switch e := e.(type) {
	case *grammar.Group:
		return v.elems(e.Expr)
	case *grammar.Seq:
		var out []valueElem
		for _, it := range e.Items {
			out = append(out, v.elems(it)...)
		}
		return out
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Atomic:
		return []valueElem{{t: tyStringNode}}
	case *grammar.Ref:
		return []valueElem{{t: ruleTy{e.Name}}}
	case *grammar.Choice:
		var ts []ty
		valueless := false
		count := map[string]int{}
		var all []capIndex
		for _, alt := range e.Alts {
			el, ok := v.single(alt)
			if !ok {
				valueless = true
				continue
			}
			ts = append(ts, el.t)
			for _, c := range el.caps {
				count[c.name]++
				all = append(all, c)
			}
		}
		if len(ts) == 0 {
			return nil
		}
		t := vunion(ts...)
		if valueless {
			t = vopt(t)
		}
		// A name captured in only some alternatives is nil when another matched.
		var caps []capIndex
		for _, c := range all {
			if count[c.name] < len(e.Alts) {
				c.t = vopt(c.t)
			}
			caps = mergeCap(caps, c)
		}
		return []valueElem{{t: t, caps: caps}}
	case *grammar.Optional:
		el, ok := v.single(e.Expr)
		if !ok {
			return nil
		}
		el.caps = slices.Clone(el.caps)
		for i := range el.caps {
			el.caps[i].t = vopt(el.caps[i].t)
		}
		el.t = vopt(el.t)
		return []valueElem{el}
	case *grammar.Repeat:
		return []valueElem{{t: nodeTy{value: listTy{v.scopeValue(e.Expr)}}}}
	case *grammar.Capture:
		el, ok := v.single(e.Expr)
		if !ok {
			el = valueElem{t: nodeTy{value: tupleTy{elems: []ty{}}}}
		}
		el.caps = append([]capIndex{{name: e.Name, paths: [][]int{{}}, t: el.t}}, el.caps...)
		return []valueElem{el}
	case *grammar.Attributed:
		elems := v.elems(e.Expr)
		for _, a := range e.Attrs {
			if a.Name != "recover" && a.Name != "missing" {
				continue
			}
			// What recovers is one element: its value or an error.
			el, ok := oneElem(elems, ungroupExpr(e.Expr))
			if !ok {
				el = valueElem{t: nodeTy{value: tupleTy{elems: []ty{}}}}
			}
			el.t = vunion(el.t, tyErr)
			elems = []valueElem{el}
		}
		return elems
	case *grammar.InlineAction:
		// Inline actions are not implemented; their values are not known yet.
		return []valueElem{{t: tyTop}}
	}
	// Top, bottom, anchors, lookaheads, discards, the cut and predicates have no value.
	return nil
}

// mergeCap adds c to caps, joining the types and paths of captures of the same name.
func mergeCap(caps []capIndex, c capIndex) []capIndex {
	for i, o := range caps {
		if o.name != c.name {
			continue
		}
		caps[i].t = vunion(o.t, c.t)
		paths := slices.Clone(o.paths)
		for _, p := range c.paths {
			if !slices.ContainsFunc(paths, func(q []int) bool { return slices.Equal(p, q) }) {
				paths = append(paths, p)
			}
		}
		caps[i].paths = paths
		return caps
	}
	return append(caps, c)
}

// operatorsRule is the union of the operand values and of the values of the operator lines: an
// infix line without an action is node[(L, Op, R)], a prefix line node[(Op, R)] and a postfix line
// node[(L, Op)], with the captures lhs, op and rhs.
func (v *valueInference) operatorsRule(rd *grammar.RuleDef, pr *grammar.OperatorsRule) ty {
	self := ruleTy{rd.Name}
	var ts []ty
	for _, o := range pr.Operands {
		if o.Action != nil {
			ts = append(ts, tyTop)
			continue
		}
		ts = append(ts, v.scopeValue(o.Expr))
	}
	for _, l := range pr.Levels {
		for _, o := range l.Operators {
			v.control.poll()
			if o.Action != nil {
				ts = append(ts, tyTop)
				continue
			}
			op, ok := v.single(o.Expr)
			if !ok {
				op = valueElem{t: nodeTy{value: tupleTy{elems: []ty{}}}}
			}
			var elems []valueElem
			switch o.Kind {
			case "prefix":
				elems = []valueElem{{t: op.t, caps: []capIndex{{name: "op", paths: [][]int{{}}, t: op.t}}}, {t: self, caps: []capIndex{{name: "rhs", paths: [][]int{{}}, t: self}}}}
			case "postfix":
				elems = []valueElem{{t: self, caps: []capIndex{{name: "lhs", paths: [][]int{{}}, t: self}}}, {t: op.t, caps: []capIndex{{name: "op", paths: [][]int{{}}, t: op.t}}}}
			default:
				elems = []valueElem{{t: self, caps: []capIndex{{name: "lhs", paths: [][]int{{}}, t: self}}}, {t: op.t, caps: []capIndex{{name: "op", paths: [][]int{{}}, t: op.t}}}, {t: self, caps: []capIndex{{name: "rhs", paths: [][]int{{}}, t: self}}}}
			}
			ts = append(ts, tupleNode(elems))
		}
	}
	return vunion(ts...)
}

// fromLegacy maps a type of the current model to the value model: reserved node kinds become
// nodes, terminal types string nodes, and the recovery kinds error.
func fromLegacy(t ty) ty {
	switch t := t.(type) {
	case nil:
		return tyTop
	case basicTy:
		switch t {
		case tyAny:
			return tyTop
		case tyNever:
			return tyBottom
		case tyNode:
			return nodeTy{value: tyTop}
		case tyTerminal:
			return tyStringNode
		}
		return t
	case namedTy:
		switch {
		case t == tyMatch:
			return tyStringNode
		case t == tyError || t == tyMissing:
			return tyErr
		case t.kind == 't':
			return tyStringNode
		case t.kind == 'c':
			return nodeTy{value: tyTop}
		}
		return t
	case listTy:
		return listTy{fromLegacy(t.elem)}
	case optTy:
		return vopt(fromLegacy(t.elem))
	case unionTy:
		ts := make([]ty, len(t.alts))
		for i, a := range t.alts {
			ts[i] = fromLegacy(a)
		}
		return vunion(ts...)
	case recordTy:
		return nodeTy{value: tyTop}
	}
	return tyTop
}

// ungroupExpr returns e without its enclosing parentheses.
func ungroupExpr(e grammar.Expr) grammar.Expr {
	for {
		g, ok := e.(*grammar.Group)
		if !ok {
			return e
		}
		e = g.Expr
	}
}
