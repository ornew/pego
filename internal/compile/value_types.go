package compile

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ornew/zeroparse/grammar"
)

// This file infers the types of rule values in the value model, in which a rule produces an
// ordinary typed value and the syntax tree is built from one generic node[T, C]. These types are
// the types the checker checks the grammar with (checkTypes).

// Types of the value model, beside basicTy, namedTy, listTy and optTy.
const (
	TyTop    BasicTy = "_"   // every value
	TyBottom BasicTy = "_|_" // no value
	TyErr    BasicTy = "error"
)

// TupleTy is a tuple (T, U, ...).
type TupleTy struct{ Elems []Ty }

// String returns the type as (T, U, ...), with a trailing comma for one element.
func (t TupleTy) String() string {
	parts := make([]string, len(t.Elems))
	for i, e := range t.Elems {
		parts[i] = e.String()
	}
	if len(parts) == 1 {
		return "(" + parts[0] + ",)"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// CapIndex locates a capture in the value of a node. In a path, the first index is an element of
// the node's tuple, and each further index an element of the tuple in the node at the previous
// one. A capture in a positive lookahead has no element: its value is kept beside the tuple, which
// side records. A capture in choice alternatives of different shapes has a location for each
// shape; the alternative that matched selects one at run time. A capture inside an optional
// expression or in only some alternatives may not be made; maybe records that, and set is then its
// type when it is made, while t also admits nil.
type CapIndex struct {
	Name  string
	Paths [][]int
	Side  bool
	T     Ty
	maybe bool
	set   Ty
}

// NodeTy is node[T, C]: a node whose Value has type value, with the captures caps. A named node
// type (type N = node[T]) has the name N and no captures: it is a distinct type over node[T].
type NodeTy struct {
	Value Ty
	Caps  []CapIndex // sorted by name
	Name  string
}

// Underlying returns the unnamed node type of a named node type, and t itself otherwise.
func (t NodeTy) Underlying() NodeTy {
	t.Name = ""
	return t
}

// String returns the name of a named node type, and the value-model term of another node type.
func (t NodeTy) String() string {
	if t.Name != "" {
		return t.Name
	}
	if t.Value == TyTop && len(t.Caps) == 0 {
		return "node[_]"
	}
	if len(t.Caps) == 0 {
		return "node[" + t.Value.String() + "]"
	}
	return "node[" + t.Value.String() + ", " + t.capsText() + "]"
}

// capsText is the text of the captures of t, as in node[T, {"name": 0.1}]. A name captured
// several times in one match is listed once, with the locations of all its captures.
func (t NodeTy) capsText() string {
	var parts []string
	at := map[string]int{}
	for _, c := range t.Caps {
		var paths []string
		for _, path := range c.Paths {
			idx := make([]string, len(path))
			for k, p := range path {
				idx[k] = strconv.Itoa(p)
			}
			paths = append(paths, strings.Join(idx, "."))
		}
		if c.Side {
			paths = append(paths, "-")
		}
		if i, ok := at[c.Name]; ok {
			parts[i] += " | " + strings.Join(paths, " | ")
			continue
		}
		at[c.Name] = len(parts)
		parts = append(parts, strconv.Quote(c.Name)+": "+strings.Join(paths, " | "))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// RuleTy is the value type of a rule, referred to by the rule's name.
type RuleTy struct{ Name string }

// String returns the rule's name.
func (t RuleTy) String() string { return t.Name }

// Vjoin is the join of types in the value model: nested joins are flattened, duplicates and
// _|_ removed, and a join with _ is _. Since T <= *T and nil <= *T, a join with an optional
// argument or nil is the optional of the join of the other arguments without their *. A join
// with a value not known yet (tyAny) is not known either.
func Vjoin(ts ...Ty) Ty {
	var alts []Ty
	seen := map[string]bool{}
	optional, null := false, false
	var add func(t Ty)
	add = func(t Ty) {
		switch u := t.(type) {
		case JoinTy:
			for _, a := range u.Alts {
				add(a)
			}
			return
		case OptTy:
			optional = true
			add(u.Elem)
			return
		case BasicTy:
			if u == TyNil {
				optional, null = true, true
				return
			}
		}
		// Rule and type names are separate namespaces, so the key includes the kind of type.
		key := fmt.Sprintf("%T %s", t, t)
		if t == nil || t == TyBottom || seen[key] {
			return
		}
		seen[key] = true
		alts = append(alts, t)
	}
	for _, t := range ts {
		add(t)
	}
	if slices.Contains(alts, Ty(TyAny)) {
		return TyAny
	}
	if slices.Contains(alts, Ty(TyTop)) {
		return TyTop
	}
	if optional {
		if len(alts) == 0 && null {
			return TyNil
		}
		if len(alts) == 0 {
			return TyBottom
		}
		return OptTy{Vjoin(alts...)}
	}
	switch len(alts) {
	case 0:
		return TyBottom
	case 1:
		return alts[0]
	}
	return JoinTy{alts}
}

// vopt is *t; an optional value of an optional type is not nested, and *_|_ is _|_.
func vopt(t Ty) Ty {
	if t == TyTop || t == TyBottom {
		return t
	}
	return Vjoin(OptTy{t})
}

// TyStringNode is node[string], the type of a match.
var TyStringNode = NodeTy{Value: TyString}

// valueElem is an element of the tuple an expression contributes to its scope.
type valueElem struct {
	t    Ty
	caps []CapIndex // the captures in this element, with paths relative to it
}

// ValueInference computes value types. Rules are computed on demand, so that the type of a rule
// referred to from an action is known when the action is checked. The rules of a cycle of
// references are computed together as the least fixed point of their values.
type ValueInference struct {
	Prog    *Program
	control *Control
	Rules   map[string]*grammar.RuleDef
	Out     map[string]Ty
	// cycle maps each rule that refers to itself, directly or through other rules, to the rules
	// of its cycle by name, and approx holds the values of the rules of the cycles being
	// computed.
	cycle  map[string][]string
	Approx map[string]Ty
	// Computing holds the rules outside cycles that are being computed.
	Computing map[string]bool
	// errs holds the type errors of the rule being computed, and final those of computed rules.
	errs, final ErrorList
	Quiet       bool // while a lambda body is typed again with a wider parameter type
	Rule        *grammar.RuleDef
	// spliced holds the splice rules whose bodies are being expanded; a call inside them is an
	// element of its own (Rule.Splice).
	spliced map[string]bool
	// nodeMembers, if not nil, receives the member accesses that read only node members.
	nodeMembers map[*grammar.Member]bool
	// rec, if not nil, receives the types of terms, scopes and rules.
	rec *ValueRecord
}

// ValueRecord holds the types the value inference gives the parts of a program, for the backends
// (Core IR terms and capture slots). Each part has the type of its last typing: terms typed again
// quietly, with a wider lambda parameter, and the rules of a cycle, typed again in each round,
// keep the final one.
type ValueRecord struct {
	// Terms holds the type of each action, predicate and binding term and of their subterms.
	Terms map[grammar.Term]Ty
	// Proven reports, for each node type constructor that converts a value, that the conversion
	// is proven: every value of the operand converts, without a check at run time.
	Proven map[*grammar.New]bool
	// Scopes holds, for the body of each capture scope (a rule body, a repetition element with
	// captures of its own, an Operators Rule line), the type of each capture a term reads.
	Scopes map[grammar.Expr]map[string]Ty
	// Rules holds the value type of every rule, splice rules included.
	Rules map[string]Ty
}

func newValueRecord() *ValueRecord {
	return &ValueRecord{Terms: map[grammar.Term]Ty{}, Proven: map[*grammar.New]bool{}, Scopes: map[grammar.Expr]map[string]Ty{}}
}

// scope records the capture types of the scope with the body e and the captures caps.
func (v *ValueInference) scope(e grammar.Expr, caps []CapIndex) {
	if v.rec == nil || v.Quiet {
		return
	}
	types := map[string]Ty{}
	for _, c := range caps {
		if _, ok := types[c.Name]; !ok {
			types[c.Name], _ = CapType(caps, c.Name)
		}
	}
	v.rec.Scopes[e] = types
}

// ValueTypes returns the value type of each rule of a type-checked program. It is computed on
// demand: nothing uses it for compilation yet.
func (p *Program) ValueTypes() map[string]Ty {
	if p.Typed == nil {
		return nil
	}
	p.Typed.valuesOnce.Do(func() {
		p.Typed.Values, p.Typed.valueErrs = ValueTypes(p, nil)
	})
	return p.Typed.Values
}

// ValueType is ValueType.
func (p *Program) ValueType(name string) (string, bool) {
	t, ok := p.ValueTypes()[name]
	if !ok {
		return "", false
	}
	return t.String(), true
}

// valueErrors returns the errors of actions in the value model of a type-checked program.
func (p *Program) valueErrors() ErrorList {
	if p.ValueTypes() == nil {
		return nil
	}
	return p.Typed.valueErrs
}

// ValueTypes returns the value type of each rule, and the errors of the actions.
func ValueTypes(p *Program, control *Control) (map[string]Ty, ErrorList) {
	return valueTypesChecked(p, control, nil, nil)
}

// valueTypesChecked is valueTypes recording the node member accesses in nodeMembers and the
// types of the program's parts in rec, each if not nil.
func valueTypesChecked(p *Program, control *Control, nodeMembers map[*grammar.Member]bool, rec *ValueRecord) (map[string]Ty, ErrorList) {
	v := &ValueInference{Prog: p, control: control, Rules: map[string]*grammar.RuleDef{}, Out: map[string]Ty{}, Approx: map[string]Ty{}, Computing: map[string]bool{}, nodeMembers: nodeMembers, rec: rec}
	rules := p.Grammar.Rules()
	for _, rd := range rules {
		v.Rules[rd.Name] = rd
	}
	v.cycle = ruleCycles(rules, v.Rules, control)
	for _, rd := range rules {
		control.poll()
		v.ruleValue(rd.Name)
	}
	if rec != nil {
		rec.Rules = maps.Clone(v.Out)
	}
	return pruneSpliceTypes(p, rules, v.Out), v.final
}

// pruneSpliceTypes returns the value types of the rules that have a type of their own: a splice
// rule makes no node, so it has a type only where a recursive call of it is an element of its own,
// and then only because another type refers to it.
func pruneSpliceTypes(p *Program, rules []*grammar.RuleDef, out map[string]Ty) map[string]Ty {
	var queue []string
	kept := map[string]bool{}
	for _, rd := range rules {
		if _, splice := p.SpliceBody(rd.Name); !splice {
			kept[rd.Name] = true
			queue = append(queue, rd.Name)
		}
	}
	if len(kept) == len(rules) {
		return out
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		t, ok := out[name]
		if !ok {
			continue
		}
		walkTy(t, func(u Ty) {
			if r, ok := u.(RuleTy); ok && !kept[r.Name] {
				kept[r.Name] = true
				queue = append(queue, r.Name)
			}
		})
	}
	pruned := make(map[string]Ty, len(kept))
	for name, t := range out {
		if kept[name] {
			pruned[name] = t
		}
	}
	return pruned
}

// walkTy calls visit on t and on every type it is made of, including the types of the captures of
// a node type.
func walkTy(t Ty, visit func(Ty)) {
	visit(t)
	switch u := t.(type) {
	case TupleTy:
		for _, e := range u.Elems {
			walkTy(e, visit)
		}
	case NodeTy:
		walkTy(u.Value, visit)
		for _, c := range u.Caps {
			if c.T != nil {
				walkTy(c.T, visit)
			}
			if c.set != nil {
				walkTy(c.set, visit)
			}
		}
	case ListTy:
		walkTy(u.Elem, visit)
	case OptTy:
		walkTy(u.Elem, visit)
	case JoinTy:
		for _, a := range u.Alts {
			walkTy(a, visit)
		}
	case RecordTy:
		for _, f := range u.fields {
			walkTy(f, visit)
		}
	}
}

// mapTy returns t with the types for which f reports a replacement replaced; it does not look
// inside a replacement. The captures of a node type are not mapped: they are not part of its text.
func mapTy(t Ty, f func(Ty) (Ty, bool)) Ty {
	if r, ok := f(t); ok {
		return r
	}
	switch u := t.(type) {
	case TupleTy:
		elems := make([]Ty, len(u.Elems))
		for i, e := range u.Elems {
			elems[i] = mapTy(e, f)
		}
		return TupleTy{elems}
	case NodeTy:
		u.Value = mapTy(u.Value, f)
		return u
	case ListTy:
		return ListTy{mapTy(u.Elem, f)}
	case OptTy:
		return OptTy{mapTy(u.Elem, f)}
	case JoinTy:
		alts := make([]Ty, len(u.Alts))
		for i, a := range u.Alts {
			alts[i] = mapTy(a, f)
		}
		return JoinTy{alts}
	case RecordTy:
		fields := make(map[string]Ty, len(u.fields))
		for k, e := range u.fields {
			fields[k] = mapTy(e, f)
		}
		return RecordTy{fields}
	}
	return t
}

// ruleCycles returns the strongly connected components of the references between rules that
// have a cycle, for each of their rules. The value of a rule can only depend on the rules it
// refers to, directly or through other rules, since a rule type comes from a reference.
func ruleCycles(rules []*grammar.RuleDef, byName map[string]*grammar.RuleDef, control *Control) map[string][]string {
	refs := map[string][]string{}
	for _, rd := range rules {
		control.poll()
		walkExprControlled(rd.Expr, func(e grammar.Expr) {
			control.poll()
			if r, ok := e.(*grammar.Ref); ok && byName[r.Name] != nil {
				refs[rd.Name] = append(refs[rd.Name], r.Name)
			}
		}, control)
	}
	// Tarjan's algorithm.
	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	out := map[string][]string{}
	var visit func(n string)
	visit = func(n string) {
		control.poll()
		index[n], low[n] = len(index), len(index)
		stack = append(stack, n)
		onStack[n] = true
		self := false
		for _, m := range refs[n] {
			self = self || m == n
			if _, ok := index[m]; !ok {
				visit(m)
				low[n] = min(low[n], low[m])
			} else if onStack[m] {
				low[n] = min(low[n], index[m])
			}
		}
		if low[n] != index[n] {
			return
		}
		i := len(stack) - 1
		for stack[i] != n {
			i--
		}
		scc := slices.Clone(stack[i:])
		stack = stack[:i]
		for _, m := range scc {
			onStack[m] = false
		}
		if len(scc) == 1 && !self {
			return
		}
		slices.Sort(scc)
		for _, m := range scc {
			out[m] = scc
		}
	}
	for _, rd := range rules {
		if _, ok := index[rd.Name]; !ok {
			visit(rd.Name)
		}
	}
	return out
}

// maxCycleRounds bounds the rounds of a cycle; the values that still change after it are not
// known.
const maxCycleRounds = 8

// ruleValue returns the value type of the rule name.
func (v *ValueInference) ruleValue(name string) Ty {
	if t, ok := v.Out[name]; ok {
		return t
	}
	if t, ok := v.Approx[name]; ok {
		return t
	}
	rd := v.Rules[name]
	if rd == nil || v.Computing[name] {
		return TyAny
	}
	rule, quiet, errs := v.Rule, v.Quiet, v.errs
	defer func() { v.Rule, v.Quiet, v.errs = rule, quiet, errs }()
	v.Quiet = false
	cycle := v.cycle[name]
	if cycle == nil {
		v.Computing[name] = true
		v.Rule, v.errs = rd, nil
		t := v.ruleDef(rd)
		delete(v.Computing, name)
		v.final = append(v.final, v.errs...)
		v.Out[name] = t
		return t
	}
	// The rules of the cycle start with no value, _|_, and each round computes every rule from
	// the values of the previous round, so that the result does not depend on the order of the
	// rules. The errors are those of the last round, which sees the final values.
	for _, m := range cycle {
		v.Approx[m] = TyBottom
	}
	var mine ErrorList
	for round := 0; ; round++ {
		next := make(map[string]Ty, len(cycle))
		mine = nil
		for _, m := range cycle {
			v.Rule, v.errs = v.Rules[m], nil
			next[m] = Vjoin(v.Approx[m], v.ruleDef(v.Rules[m]))
			mine = append(mine, v.errs...)
		}
		changed := false
		for _, m := range cycle {
			if !reflect.DeepEqual(next[m], v.Approx[m]) {
				changed = true
				if round >= maxCycleRounds {
					next[m] = TyAny
				}
			}
		}
		if !changed || round > maxCycleRounds {
			break
		}
		for _, m := range cycle {
			v.Approx[m] = next[m]
		}
	}
	for _, m := range cycle {
		v.Out[m] = v.Approx[m]
		delete(v.Approx, m)
	}
	v.final = append(v.final, mine...)
	return v.Out[name]
}

// spliceElems returns the elements a call of the splice rule name contributes: those of its body,
// without the captures, which end with the rule.
func (v *ValueInference) spliceElems(name string, body grammar.Expr) []valueElem {
	if v.spliced == nil {
		v.spliced = map[string]bool{}
	}
	v.spliced[name] = true
	defer delete(v.spliced, name)
	var out []valueElem
	for _, el := range v.elems(body) {
		if el.t != nil {
			out = append(out, valueElem{t: el.t})
		}
	}
	return out
}

func (v *ValueInference) ruleDef(rd *grammar.RuleDef) Ty {
	if pr, ok := rd.Expr.(*grammar.OperatorsRule); ok {
		return v.operatorsRule(rd, pr)
	}
	env := &valueEnv{caps: tupleNode(v.elems(rd.Expr)).Caps}
	v.scope(rd.Expr, env.caps)
	v.predicates(rd.Expr, env)
	if _, ok := v.Prog.SpliceBody(rd.Name); ok {
		// The value of a rule that makes no node of its own is its one element, as a call takes it.
		if v.spliced == nil {
			v.spliced = map[string]bool{}
		}
		v.spliced[rd.Name] = true
		defer delete(v.spliced, rd.Name)
		el, ok := v.single(rd.Expr)
		if !ok {
			return NodeTy{Value: TupleTy{Elems: []Ty{}}}
		}
		return el.t
	}
	if rd.Action == nil {
		return v.declared(rd, v.ScopeValue(rd.Expr))
	}
	return v.declared(rd, v.term(rd.Action, env))
}

// predicates types the terms of the predicates in e, which see the captures of env. A repetition
// element with a scope of its own, and the body of an inline action, see their own captures before
// those of the enclosing scope.
func (v *ValueInference) predicates(e grammar.Expr, env *valueEnv) {
	v.control.poll()
	inner := func(body grammar.Expr) *valueEnv {
		caps := tupleNode(v.elems(body)).Caps
		v.scope(body, caps)
		return &valueEnv{caps: append(slices.Clone(caps), env.caps...), locals: env.locals}
	}
	switch e := e.(type) {
	case *grammar.Predicate:
		if a, ok := e.Term.(*grammar.Bind); ok {
			t := v.term(a.Value, env)
			if d := v.Prog.Vars.lookup(a.Name); d != nil {
				switch {
				case !v.assignable(t, d.Typ):
					v.errorf(a.Diagnostic(), "cannot use %s as %s in the binding of variable %s%s", v.display(t), d.Typ, a.Name, v.castHint(t, d.Typ))
				case v.needsCast(t, d.Typ):
					v.errorf(a.Diagnostic(), "cannot use %s as %s in the binding of variable %s: the type of the value is not known; convert it with a cast, as in value.(%s)", v.display(t), d.Typ, a.Name, d.Typ)
				}
			}
		} else {
			v.term(e.Term, env)
		}
	case *grammar.Repeat:
		if elementScopedControlled(e.Expr, v.control) {
			v.predicates(e.Expr, inner(e.Expr))
		} else {
			v.predicates(e.Expr, env)
		}
	case *grammar.InlineAction:
		v.predicates(e.Expr, inner(e.Expr))
	case *grammar.Seq:
		for _, it := range e.Items {
			v.predicates(it, env)
		}
	case *grammar.Choice:
		for _, alt := range e.Alts {
			v.predicates(alt, env)
		}
	case *grammar.Recovery:
		v.predicates(e.Expr, env)
		v.predicates(e.Skip, env)
	case *grammar.Group:
		v.predicates(e.Expr, env)
	case *grammar.Optional:
		v.predicates(e.Expr, env)
	case *grammar.And:
		v.predicates(e.Expr, env)
	case *grammar.Not:
		v.predicates(e.Expr, env)
	case *grammar.Atomic:
		v.predicates(e.Expr, env)
	case *grammar.Stream:
		v.predicates(e.Expr, env)
	case *grammar.Discard:
		v.predicates(e.Expr, env)
	case *grammar.Capture:
		v.predicates(e.Expr, env)
	case *grammar.Attributed:
		v.predicates(e.Expr, env)
		// An expression argument, such as the skip expression of #recover, is a scope of its own.
		for _, a := range e.Attrs {
			v.control.poll()
			for _, arg := range v.control.expressionArgs(a) {
				v.predicates(arg, &valueEnv{caps: tupleNode(v.elems(arg)).Caps})
			}
		}
	}
}

// ScopeValue is the value of e as the body of a scope: a rule body, an operand or a repetition
// element. A scope whose body is a sequence, or that has captures, is a node of its
// tuple; otherwise its one element is its value, and a scope without elements is node[()].
func (v *ValueInference) ScopeValue(e grammar.Expr) Ty {
	elems := v.elems(e)
	seq := v.isSeq(e)
	if !seq && len(elems) == 1 && len(elems[0].caps) == 0 {
		return elems[0].t
	}
	return tupleNode(elems)
}

// tupleNode is the node of the tuple of elems, with their captures. An element without a type
// holds only captures kept beside the tuple.
func tupleNode(elems []valueElem) NodeTy {
	n := NodeTy{Value: TupleTy{Elems: []Ty{}}}
	var ts []Ty
	for _, el := range elems {
		if el.t == nil {
			n.Caps = append(n.Caps, el.caps...)
			continue
		}
		for _, c := range el.caps {
			var paths [][]int
			for _, path := range c.Paths {
				paths = append(paths, append([]int{len(ts)}, path...))
			}
			n.Caps = append(n.Caps, CapIndex{c.Name, paths, c.Side, c.T, c.maybe, c.set})
		}
		ts = append(ts, el.t)
	}
	if len(ts) > 0 {
		n.Value = TupleTy{ts}
	}
	sort.SliceStable(n.Caps, func(i, j int) bool { return n.Caps[i].Name < n.Caps[j].Name })
	return n
}

// single is the value of e where it takes one element: an alternative, the operand of ? or a
// capture. Several elements become the node of their tuple. ok is false when e has no value;
// the result then holds the captures kept beside the tuple in e. The captures of the result may be
// changed without changing its type.
func (v *ValueInference) single(e grammar.Expr) (valueElem, bool) {
	return oneElem(v.elems(e), v.isSeq(e))
}

func oneElem(elems []valueElem, seq bool) (valueElem, bool) {
	var vals []valueElem
	var side []CapIndex
	for _, el := range elems {
		if el.t == nil {
			side = append(side, el.caps...)
		} else {
			vals = append(vals, el)
		}
	}
	switch {
	case len(vals) == 0:
		return valueElem{caps: side}, false
	case len(vals) == 1 && !seq:
		el := vals[0]
		el.caps = append(slices.Clone(el.caps), side...)
		return el, true
	}
	n := tupleNode(elems)
	return valueElem{t: n, caps: slices.Clone(n.Caps)}, true
}

// isSeq is isSeq (value_shape.go) for the elements elems returns: a call of a splice rule whose
// body is being expanded is one element, the rule's own node, not the sequence of its body.
func (v *ValueInference) isSeq(e grammar.Expr) bool {
	var seen map[string]bool
	for {
		switch x := ungroupExpr(e).(type) {
		case *grammar.Seq:
			return true
		case *grammar.Attributed:
			// An attribute other than recovery only labels e; recovery makes it one element.
			if recovers(x) {
				return false
			}
			e = x.Expr
		case *grammar.Ref:
			body, ok := v.Prog.SpliceBody(x.Name)
			if !ok || v.spliced[x.Name] || seen[x.Name] {
				return false
			}
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[x.Name] = true
			e = body
		default:
			return false
		}
	}
}

// elems returns the elements e contributes to the tuple of its scope.
func (v *ValueInference) elems(e grammar.Expr) []valueElem {
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
		return []valueElem{{t: TyStringNode}}
	case *grammar.Ref:
		if body, ok := v.Prog.SpliceBody(e.Name); ok && !v.spliced[e.Name] {
			return v.spliceElems(e.Name, body)
		}
		return []valueElem{{t: RuleTy{e.Name}}}
	case *grammar.Choice:
		var ts []Ty
		valueless := false
		count := map[string]int{}
		var all []CapIndex
		alts := 0
		for _, alt := range e.Alts {
			if neverMatches(alt) {
				continue // _|_ | T is T
			}
			alts++
			el, ok := v.single(alt)
			if ok {
				ts = append(ts, el.t)
			} else {
				valueless = true
			}
			for _, c := range el.caps {
				count[c.Name]++
				all = append(all, c)
			}
		}
		var t Ty // no element when no alternative has a value
		if len(ts) > 0 {
			t = Vjoin(ts...)
			if valueless {
				t = vopt(t)
			}
		} else if len(all) == 0 {
			return nil
		}
		// A name captured in only some alternatives is nil when another matched.
		var caps []CapIndex
		for _, c := range all {
			if count[c.Name] < alts {
				c = maybeCap(c)
			}
			caps = mergeCap(caps, c)
		}
		return []valueElem{{t: t, caps: caps}}
	case *grammar.Optional:
		el, ok := v.single(e.Expr)
		if !ok && len(el.caps) == 0 {
			return nil
		}
		el.caps = slices.Clone(el.caps)
		for i := range el.caps {
			el.caps[i] = maybeCap(el.caps[i])
		}
		if ok {
			el.t = vopt(el.t)
		}
		return []valueElem{el}
	case *grammar.Repeat:
		return []valueElem{{t: NodeTy{Value: ListTy{v.ScopeValue(e.Expr)}}}}
	case *grammar.Capture:
		el, ok := v.single(e.Expr)
		if !ok {
			el.t = NodeTy{Value: TupleTy{Elems: []Ty{}}}
		}
		el.caps = append([]CapIndex{{Name: e.Name, Paths: [][]int{{}}, T: el.t}}, el.caps...)
		return []valueElem{el}
	case *grammar.Attributed:
		elems := v.elems(e.Expr)
		for _, a := range e.Attrs {
			if a.Name != "recover" && a.Name != "missing" {
				continue
			}
			// What recovers is one element: its value or an error, which #missing makes a missing
			// token.
			el, ok := oneElem(elems, v.isSeq(e.Expr))
			switch {
			case neverMatches(e.Expr):
				el = valueElem{t: TyBottom} // only the error
			case !ok:
				el.t = NodeTy{Value: TupleTy{Elems: []Ty{}}}
			}
			if a.Name == "missing" {
				el.t = Vjoin(el.t, TyMissing)
			} else {
				el.t = Vjoin(el.t, TyErr)
			}
			elems = []valueElem{el}
		}
		return elems
	case *grammar.And:
		// The captures of a positive lookahead have no element; their values are kept beside the
		// tuple.
		var caps []CapIndex
		for _, c := range tupleNode(v.elems(e.Expr)).Caps {
			caps = append(caps, CapIndex{Name: c.Name, Side: true, T: c.T})
		}
		if len(caps) == 0 {
			return nil
		}
		return []valueElem{{caps: caps}}
	case *grammar.InlineAction:
		// Inline actions are lowered to rules (inline_actions.go) before value types are computed.
		return []valueElem{{t: TyAny}}
	}
	// Top, bottom, anchors, negative lookaheads, discards, the cut and predicates have no value.
	return nil
}

// neverMatches reports whether e is _|_, which has no value since it never matches.
func neverMatches(e grammar.Expr) bool {
	switch e := ungroupExpr(e).(type) {
	case *grammar.Bottom:
		return true
	case *grammar.Attributed:
		for _, a := range e.Attrs {
			if a.Name == "recover" || a.Name == "missing" {
				return false // recovery produces an error value
			}
		}
		return neverMatches(e.Expr)
	}
	return false
}

// maybeCap returns c for a capture that may not be made.
func maybeCap(c CapIndex) CapIndex {
	if !c.maybe {
		c.maybe, c.set = true, c.T
	}
	c.T = vopt(c.T)
	return c
}

// mergeCap adds c to caps, joining the types and locations of captures of the same name.
func mergeCap(caps []CapIndex, c CapIndex) []CapIndex {
	for i, o := range caps {
		if o.Name != c.Name {
			continue
		}
		caps[i].T = Vjoin(o.T, c.T)
		if o.maybe || c.maybe {
			caps[i].maybe, caps[i].set = true, Vjoin(setType(o), setType(c))
		}
		paths := slices.Clone(o.Paths)
		for _, p := range c.Paths {
			if !slices.ContainsFunc(paths, func(q []int) bool { return slices.Equal(p, q) }) {
				paths = append(paths, p)
			}
		}
		caps[i].Paths = paths
		caps[i].Side = o.Side || c.Side
		return caps
	}
	return append(caps, c)
}

// setType is the type of the capture c when it is made.
func setType(c CapIndex) Ty {
	if c.maybe {
		return c.set
	}
	return c.T
}

// operatorsRule is the join of the operand values and of the values of the operator lines: an
// infix line without an action is node[(L, Op, R)], a prefix line node[(Op, R)] and a postfix line
// node[(L, Op)], with the captures lhs, op and rhs. With a whole-rule action, it is the type of the
// action instead, which sees the folded value as $value (operatorsFold).
func (v *ValueInference) operatorsRule(rd *grammar.RuleDef, pr *grammar.OperatorsRule) Ty {
	if rd.Action != nil {
		fold := v.operatorsFold(rd, pr)
		env := &valueEnv{locals: map[string]Ty{OperatorsRuleValue: fold}}
		return v.declared(rd, v.term(rd.Action, env))
	}
	var self Ty = RuleTy{rd.Name}
	if rd.Type != nil {
		// The lhs and rhs captures are values of the rule, each of which is checked against the
		// rule's declared type.
		self = v.resolve(rd.Type)
	}
	ts, tuples, operators := v.operatorsLines(rd, pr, self, true)
	if rd.Type != nil {
		// Each line that does not fit is reported on its own.
		d, bad := v.resolve(rd.Type), false
		dn, converts := v.Prog.operatorsNodeType(rd)
		for i, t := range ts {
			if converts && operators[i] {
				// The node of an operator is converted to the named node type.
				if v.convertible(t, dn) {
					continue
				}
				bad = true
				if tuples[i] {
					v.errorf(rd.Diagnostic(), "operators of %s without an action produce a tuple node, which cannot be converted to %s", rd.Name, d)
				} else {
					v.errorf(rd.Diagnostic(), "operatorsRule %s produces %s, which cannot be converted to %s", rd.Name, v.display(t), d)
				}
				continue
			}
			if v.assignable(t, d) {
				continue
			}
			bad = true
			if tuples[i] {
				v.errorf(rd.Diagnostic(), "operators of %s without an action produce a tuple node, which is not assignable to %s", rd.Name, d)
			} else if converts && !operators[i] {
				v.errorf(rd.Diagnostic(), "operand of %s produces %s, which is not assignable to %s; an operand is not converted: declare the operand rule as %s, or declare %s as a join that includes %s", rd.Name, v.display(t), d, d, rd.Name, v.display(t))
			} else {
				v.errorf(rd.Diagnostic(), "operatorsRule %s produces %s, which is not assignable to %s", rd.Name, v.display(t), d)
			}
		}
		if bad || converts {
			return d
		}
	}
	return v.declared(rd, Vjoin(ts...))
}

// operatorsFold returns the type of the folded value of the Operators Rule rd, which has a
// whole-rule action: the join of its line values, where $lhs and $rhs have that join type. The
// declared type of the rule is the type of the action's result, so the fold has no declared type
// and no name to refer to itself by; its type is the least fixed point of the lines, computed from
// _|_ round by round. A fold that is still growing after maxCycleRounds (the tuple nodes of
// operators without actions nest without bound) gives $lhs and $rhs the widest type its values can
// have: node[_] when every line makes a node, and any otherwise.
func (v *ValueInference) operatorsFold(rd *grammar.RuleDef, pr *grammar.OperatorsRule) Ty {
	quiet := v.Quiet
	v.Quiet = true
	var self Ty = TyBottom
	stable := false
	for round := 0; round <= maxCycleRounds; round++ {
		ts, _, _ := v.operatorsLines(rd, pr, self, false)
		next := Vjoin(ts...)
		if reflect.DeepEqual(next, self) {
			stable = true
			break
		}
		self = next
	}
	if !stable {
		ts, _, _ := v.operatorsLines(rd, pr, TyBottom, false)
		self = NodeTy{Value: TyTop}
		for _, t := range ts {
			if !v.assignable(t, self) {
				self = TyAny
				break
			}
		}
	}
	v.Quiet = quiet
	// The last typing reports the errors of the lines and records their types.
	ts, _, _ := v.operatorsLines(rd, pr, self, false)
	return Vjoin(ts...)
}

// operatorsLines types the skip and the lines of the Operators Rule rd, with $lhs and $rhs of type
// self, and returns the value of each line, with the indexes of the lines that make the tuple node
// of an operator without an action (tuples) and of the operator lines (operators). If checked, the
// value of each line is checked against the rule's declared type.
func (v *ValueInference) operatorsLines(rd *grammar.RuleDef, pr *grammar.OperatorsRule, self Ty, checked bool) (ts []Ty, tuples, operators map[int]bool) {
	tuples = map[int]bool{}    // the lines whose value is the tuple node of an operator without an action
	operators = map[int]bool{} // the lines of operators, whose nodes a named node type converts
	if pr.Skip != nil {
		v.scope(pr.Skip, nil)
		v.predicates(pr.Skip, &valueEnv{})
	}
	for _, o := range pr.Operands {
		env := &valueEnv{caps: tupleNode(v.elems(o.Expr)).Caps}
		v.scope(o.Expr, env.caps)
		v.predicates(o.Expr, env)
		if o.Action != nil {
			ts = append(ts, v.term(o.Action, env))
		} else {
			ts = append(ts, v.ScopeValue(o.Expr))
		}
		if checked {
			v.checkResult(o, rd, ts[len(ts)-1])
		}
	}
	for _, l := range pr.Levels {
		for _, o := range l.Operators {
			v.control.poll()
			op, ok := v.single(o.Expr)
			if !ok {
				op = valueElem{t: NodeTy{Value: TupleTy{Elems: []Ty{}}}}
			}
			v.scope(o.Expr, op.caps)
			v.predicates(o.Expr, &valueEnv{caps: op.caps})
			if o.Action != nil {
				locals := map[string]Ty{"op": op.t}
				if o.Kind != grammar.Prefix {
					locals["lhs"] = self
				}
				if o.Kind != grammar.Postfix {
					locals["rhs"] = self
				}
				operators[len(ts)] = true
				ts = append(ts, v.term(o.Action, &valueEnv{caps: op.caps, locals: locals}))
				if checked {
					v.checkResult(o, rd, ts[len(ts)-1])
				}
				continue
			}
			var elems []valueElem
			switch o.Kind {
			case "prefix":
				elems = []valueElem{{t: op.t, caps: []CapIndex{{Name: "op", Paths: [][]int{{}}, T: op.t}}}, {t: self, caps: []CapIndex{{Name: "rhs", Paths: [][]int{{}}, T: self}}}}
			case "postfix":
				elems = []valueElem{{t: self, caps: []CapIndex{{Name: "lhs", Paths: [][]int{{}}, T: self}}}, {t: op.t, caps: []CapIndex{{Name: "op", Paths: [][]int{{}}, T: op.t}}}}
			default:
				elems = []valueElem{{t: self, caps: []CapIndex{{Name: "lhs", Paths: [][]int{{}}, T: self}}}, {t: op.t, caps: []CapIndex{{Name: "op", Paths: [][]int{{}}, T: op.t}}}, {t: self, caps: []CapIndex{{Name: "rhs", Paths: [][]int{{}}, T: self}}}}
			}
			tuples[len(ts)] = true
			operators[len(ts)] = true
			ts = append(ts, tupleNode(elems))
		}
	}
	return ts, tuples, operators
}

// FromLegacy maps a legacy type (types.go) to the value model: reserved node kinds become nodes
// and the recovery kinds error. any stays any: it is assignable to every type, and its values are
// checked at run time.
func FromLegacy(t Ty) Ty {
	switch t := t.(type) {
	case nil:
		return TyTop
	case BasicTy:
		switch t {
		case tyNever:
			return TyBottom
		case TyNode:
			return NodeTy{Value: TyTop}
		}
		return t
	case NamedTy:
		switch {
		case t == TyMatch:
			return TyStringNode
		case t == TyError || t == TyMissing:
			return TyErr
		case t.Kind == 'c':
			return NodeTy{Value: TyTop}
		}
		return t
	case ListTy:
		return ListTy{FromLegacy(t.Elem)}
	case OptTy:
		return vopt(FromLegacy(t.Elem))
	case JoinTy:
		ts := make([]Ty, len(t.Alts))
		for i, a := range t.Alts {
			ts[i] = FromLegacy(a)
		}
		return Vjoin(ts...)
	case RecordTy:
		return NodeTy{Value: TyTop}
	}
	return TyTop
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
