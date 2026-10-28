package engine

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
)

// This file infers the types of rule values in the value model, in which a rule produces an
// ordinary typed value and the syntax tree is built from one generic node[T, C]. These types are
// the types the checker checks the grammar with (checkTypes).

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
// one. A capture in a positive lookahead has no element: its value is kept beside the tuple, which
// side records. A capture in choice alternatives of different shapes has a location for each
// shape; the alternative that matched selects one at run time. A capture inside an optional
// expression or in only some alternatives may not be made; maybe records that, and set is then its
// type when it is made, while t also admits nil.
type capIndex struct {
	name  string
	paths [][]int
	side  bool
	t     ty
	maybe bool
	set   ty
}

// nodeTy is node[T, C]: a node whose Value has type value, with the captures caps. A named node
// type (type N = node[T]) has the name N and no captures: it is a distinct type over node[T].
type nodeTy struct {
	value ty
	caps  []capIndex // sorted by name
	name  string
}

// underlying returns the unnamed node type of a named node type, and t itself otherwise.
func (t nodeTy) underlying() nodeTy {
	t.name = ""
	return t
}

func (t nodeTy) String() string {
	if t.name != "" {
		return t.name
	}
	if t.value == tyTop && len(t.caps) == 0 {
		return "node[_]"
	}
	if len(t.caps) == 0 {
		return "node[" + t.value.String() + "]"
	}
	return "node[" + t.value.String() + ", " + t.capsText() + "]"
}

// capsText is the text of the captures of t, as in node[T, {"name": 0.1}]. A name captured
// several times in one match is listed once, with the locations of all its captures.
func (t nodeTy) capsText() string {
	var parts []string
	at := map[string]int{}
	for _, c := range t.caps {
		var paths []string
		for _, path := range c.paths {
			idx := make([]string, len(path))
			for k, p := range path {
				idx[k] = strconv.Itoa(p)
			}
			paths = append(paths, strings.Join(idx, "."))
		}
		if c.side {
			paths = append(paths, "-")
		}
		if i, ok := at[c.name]; ok {
			parts[i] += " | " + strings.Join(paths, " | ")
			continue
		}
		at[c.name] = len(parts)
		parts = append(parts, strconv.Quote(c.name)+": "+strings.Join(paths, " | "))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// ruleTy is the value type of a rule, referred to by the rule's name.
type ruleTy struct{ name string }

func (t ruleTy) String() string { return t.name }

// vjoin is the join of types in the value model: nested joins are flattened, duplicates and
// _|_ removed, and a join with _ is _. Since T <= *T and nil <= *T, a join with an optional
// argument or nil is the optional of the join of the other arguments without their *. A join
// with a value not known yet (tyAny) is not known either.
func vjoin(ts ...ty) ty {
	var alts []ty
	seen := map[string]bool{}
	optional, null := false, false
	var add func(t ty)
	add = func(t ty) {
		switch u := t.(type) {
		case joinTy:
			for _, a := range u.alts {
				add(a)
			}
			return
		case optTy:
			optional = true
			add(u.elem)
			return
		case basicTy:
			if u == tyNil {
				optional, null = true, true
				return
			}
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
	if slices.Contains(alts, ty(tyAny)) {
		return tyAny
	}
	if slices.Contains(alts, ty(tyTop)) {
		return tyTop
	}
	if optional {
		if len(alts) == 0 && null {
			return tyNil
		}
		if len(alts) == 0 {
			return tyBottom
		}
		return optTy{vjoin(alts...)}
	}
	switch len(alts) {
	case 0:
		return tyBottom
	case 1:
		return alts[0]
	}
	return joinTy{alts}
}

// vopt is *t; an optional value of an optional type is not nested, and *_|_ is _|_.
func vopt(t ty) ty {
	if t == tyTop || t == tyBottom {
		return t
	}
	return vjoin(optTy{t})
}

var tyStringNode = nodeTy{value: tyString}

// valueElem is an element of the tuple an expression contributes to its scope.
type valueElem struct {
	t    ty
	caps []capIndex // the captures in this element, with paths relative to it
}

// valueInference computes value types. Rules are computed on demand, so that the type of a rule
// referred to from an action is known when the action is checked. The rules of a cycle of
// references are computed together as the least fixed point of their values.
type valueInference struct {
	prog    *Program
	control *compileControl
	rules   map[string]*grammar.RuleDef
	out     map[string]ty
	// cycle maps each rule that refers to itself, directly or through other rules, to the rules
	// of its cycle by name, and approx holds the values of the rules of the cycles being
	// computed.
	cycle  map[string][]string
	approx map[string]ty
	// computing holds the rules outside cycles that are being computed.
	computing map[string]bool
	// errs holds the type errors of the rule being computed, and final those of computed rules.
	errs, final ErrorList
	quiet       bool // while a lambda body is typed again with a wider parameter type
	rule        *grammar.RuleDef
	// spliced holds the splice rules whose bodies are being expanded; a call inside them is an
	// element of its own (staticRule.splice).
	spliced map[string]bool
	// nodeMembers, if not nil, receives the member accesses that read only node members.
	nodeMembers map[*grammar.Member]bool
	// rec, if not nil, receives the types of terms, scopes and rules.
	rec *valueRecord
}

// valueRecord holds the types the value inference gives the parts of a program, for the backends
// (Core IR terms and capture slots). Each part has the type of its last typing: terms typed again
// quietly, with a wider lambda parameter, and the rules of a cycle, typed again in each round,
// keep the final one.
type valueRecord struct {
	// terms holds the type of each action, predicate and binding term and of their subterms.
	terms map[grammar.Term]ty
	// proven reports, for each node type constructor that converts a value, that the conversion
	// is proven: every value of the operand converts, without a check at run time.
	proven map[*grammar.New]bool
	// scopes holds, for the body of each capture scope (a rule body, a repetition element with
	// captures of its own, an Operators Rule line), the type of each capture a term reads.
	scopes map[grammar.Expr]map[string]ty
	// rules holds the value type of every rule, splice rules included.
	rules map[string]ty
}

func newValueRecord() *valueRecord {
	return &valueRecord{terms: map[grammar.Term]ty{}, proven: map[*grammar.New]bool{}, scopes: map[grammar.Expr]map[string]ty{}}
}

// scope records the capture types of the scope with the body e and the captures caps.
func (v *valueInference) scope(e grammar.Expr, caps []capIndex) {
	if v.rec == nil || v.quiet {
		return
	}
	types := map[string]ty{}
	for _, c := range caps {
		if _, ok := types[c.name]; !ok {
			types[c.name], _ = capType(caps, c.name)
		}
	}
	v.rec.scopes[e] = types
}

// valueTypes returns the value type of each rule of a type-checked program. It is computed on
// demand: nothing uses it for compilation yet.
func (p *Program) valueTypes() map[string]ty {
	if p.typed == nil {
		return nil
	}
	p.typed.valuesOnce.Do(func() {
		p.typed.values, p.typed.valueErrs = valueTypes(p, nil)
	})
	return p.typed.values
}

// ValueType returns the type of the value that the rule name builds in the value model (for
// example "node[(*node[string],)]"). It reports false if the program has no such rule or its
// types were not checked.
func (p *Program) ValueType(name string) (string, bool) {
	t, ok := p.valueTypes()[name]
	if !ok {
		return "", false
	}
	return t.String(), true
}

// valueErrors returns the errors of actions in the value model of a type-checked program.
func (p *Program) valueErrors() ErrorList {
	if p.valueTypes() == nil {
		return nil
	}
	return p.typed.valueErrs
}

// valueTypes returns the value type of each rule, and the errors of the actions.
func valueTypes(p *Program, control *compileControl) (map[string]ty, ErrorList) {
	return valueTypesChecked(p, control, nil, nil)
}

// valueTypesChecked is valueTypes recording the node member accesses in nodeMembers and the
// types of the program's parts in rec, each if not nil.
func valueTypesChecked(p *Program, control *compileControl, nodeMembers map[*grammar.Member]bool, rec *valueRecord) (map[string]ty, ErrorList) {
	v := &valueInference{prog: p, control: control, rules: map[string]*grammar.RuleDef{}, out: map[string]ty{}, approx: map[string]ty{}, computing: map[string]bool{}, nodeMembers: nodeMembers, rec: rec}
	rules := p.Grammar.Rules()
	for _, rd := range rules {
		v.rules[rd.Name] = rd
	}
	v.cycle = ruleCycles(rules, v.rules, control)
	for _, rd := range rules {
		control.poll()
		v.ruleValue(rd.Name)
	}
	if rec != nil {
		rec.rules = maps.Clone(v.out)
	}
	return pruneSpliceTypes(p, rules, v.out), v.final
}

// pruneSpliceTypes returns the value types of the rules that have a type of their own: a splice
// rule makes no node, so it has a type only where a recursive call of it is an element of its own,
// and then only because another type refers to it.
func pruneSpliceTypes(p *Program, rules []*grammar.RuleDef, out map[string]ty) map[string]ty {
	var queue []string
	kept := map[string]bool{}
	for _, rd := range rules {
		if _, splice := p.spliceBody(rd.Name); !splice {
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
		walkTy(t, func(u ty) {
			if r, ok := u.(ruleTy); ok && !kept[r.name] {
				kept[r.name] = true
				queue = append(queue, r.name)
			}
		})
	}
	pruned := make(map[string]ty, len(kept))
	for name, t := range out {
		if kept[name] {
			pruned[name] = t
		}
	}
	return pruned
}

// walkTy calls visit on t and on every type it is made of, including the types of the captures of
// a node type.
func walkTy(t ty, visit func(ty)) {
	visit(t)
	switch u := t.(type) {
	case tupleTy:
		for _, e := range u.elems {
			walkTy(e, visit)
		}
	case nodeTy:
		walkTy(u.value, visit)
		for _, c := range u.caps {
			if c.t != nil {
				walkTy(c.t, visit)
			}
			if c.set != nil {
				walkTy(c.set, visit)
			}
		}
	case listTy:
		walkTy(u.elem, visit)
	case optTy:
		walkTy(u.elem, visit)
	case joinTy:
		for _, a := range u.alts {
			walkTy(a, visit)
		}
	case recordTy:
		for _, f := range u.fields {
			walkTy(f, visit)
		}
	}
}

// mapTy returns t with the types for which f reports a replacement replaced; it does not look
// inside a replacement. The captures of a node type are not mapped: they are not part of its text.
func mapTy(t ty, f func(ty) (ty, bool)) ty {
	if r, ok := f(t); ok {
		return r
	}
	switch u := t.(type) {
	case tupleTy:
		elems := make([]ty, len(u.elems))
		for i, e := range u.elems {
			elems[i] = mapTy(e, f)
		}
		return tupleTy{elems}
	case nodeTy:
		u.value = mapTy(u.value, f)
		return u
	case listTy:
		return listTy{mapTy(u.elem, f)}
	case optTy:
		return optTy{mapTy(u.elem, f)}
	case joinTy:
		alts := make([]ty, len(u.alts))
		for i, a := range u.alts {
			alts[i] = mapTy(a, f)
		}
		return joinTy{alts}
	case recordTy:
		fields := make(map[string]ty, len(u.fields))
		for k, e := range u.fields {
			fields[k] = mapTy(e, f)
		}
		return recordTy{fields}
	}
	return t
}

// ruleCycles returns the strongly connected components of the references between rules that
// have a cycle, for each of their rules. The value of a rule can only depend on the rules it
// refers to, directly or through other rules, since a rule type comes from a reference.
func ruleCycles(rules []*grammar.RuleDef, byName map[string]*grammar.RuleDef, control *compileControl) map[string][]string {
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
func (v *valueInference) ruleValue(name string) ty {
	if t, ok := v.out[name]; ok {
		return t
	}
	if t, ok := v.approx[name]; ok {
		return t
	}
	rd := v.rules[name]
	if rd == nil || v.computing[name] {
		return tyAny
	}
	rule, quiet, errs := v.rule, v.quiet, v.errs
	defer func() { v.rule, v.quiet, v.errs = rule, quiet, errs }()
	v.quiet = false
	cycle := v.cycle[name]
	if cycle == nil {
		v.computing[name] = true
		v.rule, v.errs = rd, nil
		t := v.ruleDef(rd)
		delete(v.computing, name)
		v.final = append(v.final, v.errs...)
		v.out[name] = t
		return t
	}
	// The rules of the cycle start with no value, _|_, and each round computes every rule from
	// the values of the previous round, so that the result does not depend on the order of the
	// rules. The errors are those of the last round, which sees the final values.
	for _, m := range cycle {
		v.approx[m] = tyBottom
	}
	var mine ErrorList
	for round := 0; ; round++ {
		next := make(map[string]ty, len(cycle))
		mine = nil
		for _, m := range cycle {
			v.rule, v.errs = v.rules[m], nil
			next[m] = vjoin(v.approx[m], v.ruleDef(v.rules[m]))
			mine = append(mine, v.errs...)
		}
		changed := false
		for _, m := range cycle {
			if !reflect.DeepEqual(next[m], v.approx[m]) {
				changed = true
				if round >= maxCycleRounds {
					next[m] = tyAny
				}
			}
		}
		if !changed || round > maxCycleRounds {
			break
		}
		for _, m := range cycle {
			v.approx[m] = next[m]
		}
	}
	for _, m := range cycle {
		v.out[m] = v.approx[m]
		delete(v.approx, m)
	}
	v.final = append(v.final, mine...)
	return v.out[name]
}

// spliceElems returns the elements a call of the splice rule name contributes: those of its body,
// without the captures, which end with the rule.
func (v *valueInference) spliceElems(name string, body grammar.Expr) []valueElem {
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

func (v *valueInference) ruleDef(rd *grammar.RuleDef) ty {
	if pr, ok := rd.Expr.(*grammar.OperatorsRule); ok {
		return v.operatorsRule(rd, pr)
	}
	env := &valueEnv{caps: tupleNode(v.elems(rd.Expr)).caps}
	v.scope(rd.Expr, env.caps)
	v.predicates(rd.Expr, env)
	if _, ok := v.prog.spliceBody(rd.Name); ok {
		// The value of a rule that makes no node of its own is its one element, as a call takes it.
		if v.spliced == nil {
			v.spliced = map[string]bool{}
		}
		v.spliced[rd.Name] = true
		defer delete(v.spliced, rd.Name)
		el, ok := v.single(rd.Expr)
		if !ok {
			return nodeTy{value: tupleTy{elems: []ty{}}}
		}
		return el.t
	}
	if rd.Action == nil {
		return v.declared(rd, v.scopeValue(rd.Expr))
	}
	return v.declared(rd, v.term(rd.Action, env))
}

// predicates types the terms of the predicates in e, which see the captures of env. A repetition
// element with a scope of its own, and the body of an inline action, see their own captures before
// those of the enclosing scope.
func (v *valueInference) predicates(e grammar.Expr, env *valueEnv) {
	v.control.poll()
	inner := func(body grammar.Expr) *valueEnv {
		caps := tupleNode(v.elems(body)).caps
		v.scope(body, caps)
		return &valueEnv{caps: append(slices.Clone(caps), env.caps...), locals: env.locals}
	}
	switch e := e.(type) {
	case *grammar.Predicate:
		if a, ok := e.Term.(*grammar.Bind); ok {
			t := v.term(a.Value, env)
			if d := v.prog.vars.lookup(a.Name); d != nil {
				switch {
				case !v.assignable(t, d.typ):
					v.errorf(a.Diagnostic(), "cannot use %s as %s in the binding of variable %s%s", v.display(t), d.typ, a.Name, v.castHint(t, d.typ))
				case v.needsCast(t, d.typ):
					v.errorf(a.Diagnostic(), "cannot use %s as %s in the binding of variable %s: the type of the value is not known; convert it with a cast, as in value.(%s)", v.display(t), d.typ, a.Name, d.typ)
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
				v.predicates(arg, &valueEnv{caps: tupleNode(v.elems(arg)).caps})
			}
		}
	}
}

// scopeValue is the value of e as the body of a scope: a rule body, an operand or a repetition
// element. A scope whose body is a sequence, or that has captures, is a node of its
// tuple; otherwise its one element is its value, and a scope without elements is node[()].
func (v *valueInference) scopeValue(e grammar.Expr) ty {
	elems := v.elems(e)
	seq := v.isSeq(e)
	if !seq && len(elems) == 1 && len(elems[0].caps) == 0 {
		return elems[0].t
	}
	return tupleNode(elems)
}

// tupleNode is the node of the tuple of elems, with their captures. An element without a type
// holds only captures kept beside the tuple.
func tupleNode(elems []valueElem) nodeTy {
	n := nodeTy{value: tupleTy{elems: []ty{}}}
	var ts []ty
	for _, el := range elems {
		if el.t == nil {
			n.caps = append(n.caps, el.caps...)
			continue
		}
		for _, c := range el.caps {
			var paths [][]int
			for _, path := range c.paths {
				paths = append(paths, append([]int{len(ts)}, path...))
			}
			n.caps = append(n.caps, capIndex{c.name, paths, c.side, c.t, c.maybe, c.set})
		}
		ts = append(ts, el.t)
	}
	if len(ts) > 0 {
		n.value = tupleTy{ts}
	}
	sort.SliceStable(n.caps, func(i, j int) bool { return n.caps[i].name < n.caps[j].name })
	return n
}

// single is the value of e where it takes one element: an alternative, the operand of ? or a
// capture. Several elements become the node of their tuple. ok is false when e has no value;
// the result then holds the captures kept beside the tuple in e. The captures of the result may be
// changed without changing its type.
func (v *valueInference) single(e grammar.Expr) (valueElem, bool) {
	return oneElem(v.elems(e), v.isSeq(e))
}

func oneElem(elems []valueElem, seq bool) (valueElem, bool) {
	var vals []valueElem
	var side []capIndex
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
	return valueElem{t: n, caps: slices.Clone(n.caps)}, true
}

// isSeq is isSeq (value_shape.go) for the elements elems returns: a call of a splice rule whose
// body is being expanded is one element, the rule's own node, not the sequence of its body.
func (v *valueInference) isSeq(e grammar.Expr) bool {
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
			body, ok := v.prog.spliceBody(x.Name)
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
		if body, ok := v.prog.spliceBody(e.Name); ok && !v.spliced[e.Name] {
			return v.spliceElems(e.Name, body)
		}
		return []valueElem{{t: ruleTy{e.Name}}}
	case *grammar.Choice:
		var ts []ty
		valueless := false
		count := map[string]int{}
		var all []capIndex
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
				count[c.name]++
				all = append(all, c)
			}
		}
		var t ty // no element when no alternative has a value
		if len(ts) > 0 {
			t = vjoin(ts...)
			if valueless {
				t = vopt(t)
			}
		} else if len(all) == 0 {
			return nil
		}
		// A name captured in only some alternatives is nil when another matched.
		var caps []capIndex
		for _, c := range all {
			if count[c.name] < alts {
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
		return []valueElem{{t: nodeTy{value: listTy{v.scopeValue(e.Expr)}}}}
	case *grammar.Capture:
		el, ok := v.single(e.Expr)
		if !ok {
			el.t = nodeTy{value: tupleTy{elems: []ty{}}}
		}
		el.caps = append([]capIndex{{name: e.Name, paths: [][]int{{}}, t: el.t}}, el.caps...)
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
				el = valueElem{t: tyBottom} // only the error
			case !ok:
				el.t = nodeTy{value: tupleTy{elems: []ty{}}}
			}
			if a.Name == "missing" {
				el.t = vjoin(el.t, tyMissing)
			} else {
				el.t = vjoin(el.t, tyErr)
			}
			elems = []valueElem{el}
		}
		return elems
	case *grammar.And:
		// The captures of a positive lookahead have no element; their values are kept beside the
		// tuple.
		var caps []capIndex
		for _, c := range tupleNode(v.elems(e.Expr)).caps {
			caps = append(caps, capIndex{name: c.name, side: true, t: c.t})
		}
		if len(caps) == 0 {
			return nil
		}
		return []valueElem{{caps: caps}}
	case *grammar.InlineAction:
		// Inline actions are lowered to rules (inline_actions.go) before value types are computed.
		return []valueElem{{t: tyAny}}
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
func maybeCap(c capIndex) capIndex {
	if !c.maybe {
		c.maybe, c.set = true, c.t
	}
	c.t = vopt(c.t)
	return c
}

// mergeCap adds c to caps, joining the types and locations of captures of the same name.
func mergeCap(caps []capIndex, c capIndex) []capIndex {
	for i, o := range caps {
		if o.name != c.name {
			continue
		}
		caps[i].t = vjoin(o.t, c.t)
		if o.maybe || c.maybe {
			caps[i].maybe, caps[i].set = true, vjoin(setType(o), setType(c))
		}
		paths := slices.Clone(o.paths)
		for _, p := range c.paths {
			if !slices.ContainsFunc(paths, func(q []int) bool { return slices.Equal(p, q) }) {
				paths = append(paths, p)
			}
		}
		caps[i].paths = paths
		caps[i].side = o.side || c.side
		return caps
	}
	return append(caps, c)
}

// setType is the type of the capture c when it is made.
func setType(c capIndex) ty {
	if c.maybe {
		return c.set
	}
	return c.t
}

// operatorsRule is the join of the operand values and of the values of the operator lines: an
// infix line without an action is node[(L, Op, R)], a prefix line node[(Op, R)] and a postfix line
// node[(L, Op)], with the captures lhs, op and rhs.
func (v *valueInference) operatorsRule(rd *grammar.RuleDef, pr *grammar.OperatorsRule) ty {
	var self ty = ruleTy{rd.Name}
	if rd.Type != nil {
		// The lhs and rhs captures are values of the rule, each of which is checked against the
		// rule's declared type.
		self = v.resolve(rd.Type)
	}
	var ts []ty
	tuples := map[int]bool{}    // the lines whose value is the tuple node of an operator without an action
	operators := map[int]bool{} // the lines of operators, whose nodes a named node type converts
	if pr.Skip != nil {
		v.scope(pr.Skip, nil)
		v.predicates(pr.Skip, &valueEnv{})
	}
	for _, o := range pr.Operands {
		env := &valueEnv{caps: tupleNode(v.elems(o.Expr)).caps}
		v.scope(o.Expr, env.caps)
		v.predicates(o.Expr, env)
		if o.Action != nil {
			ts = append(ts, v.term(o.Action, env))
		} else {
			ts = append(ts, v.scopeValue(o.Expr))
		}
		v.checkResult(o, rd, ts[len(ts)-1])
	}
	for _, l := range pr.Levels {
		for _, o := range l.Operators {
			v.control.poll()
			op, ok := v.single(o.Expr)
			if !ok {
				op = valueElem{t: nodeTy{value: tupleTy{elems: []ty{}}}}
			}
			v.scope(o.Expr, op.caps)
			v.predicates(o.Expr, &valueEnv{caps: op.caps})
			if o.Action != nil {
				locals := map[string]ty{"op": op.t}
				if o.Kind != grammar.Prefix {
					locals["lhs"] = self
				}
				if o.Kind != grammar.Postfix {
					locals["rhs"] = self
				}
				operators[len(ts)] = true
				ts = append(ts, v.term(o.Action, &valueEnv{caps: op.caps, locals: locals}))
				v.checkResult(o, rd, ts[len(ts)-1])
				continue
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
			tuples[len(ts)] = true
			operators[len(ts)] = true
			ts = append(ts, tupleNode(elems))
		}
	}
	if rd.Type != nil {
		// Each line that does not fit is reported on its own.
		d, bad := v.resolve(rd.Type), false
		dn, converts := v.prog.operatorsNodeType(rd)
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
	return v.declared(rd, vjoin(ts...))
}

// fromLegacy maps a legacy type (types.go) to the value model: reserved node kinds become nodes
// and the recovery kinds error. any stays any: it is assignable to every type, and its values are
// checked at run time.
func fromLegacy(t ty) ty {
	switch t := t.(type) {
	case nil:
		return tyTop
	case basicTy:
		switch t {
		case tyNever:
			return tyBottom
		case tyNode:
			return nodeTy{value: tyTop}
		}
		return t
	case namedTy:
		switch {
		case t == tyMatch:
			return tyStringNode
		case t == tyError || t == tyMissing:
			return tyErr
		case t.kind == 'c':
			return nodeTy{value: tyTop}
		}
		return t
	case listTy:
		return listTy{fromLegacy(t.elem)}
	case optTy:
		return vopt(fromLegacy(t.elem))
	case joinTy:
		ts := make([]ty, len(t.alts))
		for i, a := range t.alts {
			ts[i] = fromLegacy(a)
		}
		return vjoin(ts...)
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
