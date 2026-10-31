package compile

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ornew/pego/grammar"
)

// termCtx is the context of a value expression in the walk.
type termCtx struct {
	rule   string
	c      wctx
	pred   *grammar.Predicate // the predicate that holds the term, or nil for an action
	action bool
	holder grammar.Expr     // the inline action that holds the term, or nil
	params map[string]param // the parameters of the enclosing lambdas
	lambda bool             // inside a lambda
}

type param struct {
	acc  bool    // the accumulator of foldl or foldr
	int  bool    // known to hold an int
	elem content // what an element of the list can hold
	// elems are the expressions whose values are the elements of the list, when they are known.
	elems []grammar.Expr
}

// binding is a binding of a variable in a predicate.
type binding struct {
	rule string
	b    *grammar.Bind
	c    wctx
}

// content describes what a value can hold, for the copying operations that copy it.
type content struct {
	unbounded bool           // larger than a constant of the grammar, other than through variables
	vars      bool           // the value of a variable
	rec       map[string]int // captures that hold a call on a recursion with the rule, with counts
	acc       map[string]int // fold accumulators, with counts
	outer     bool           // from outside the innermost lambda, and unbounded
}

// bounded reports whether the value is bounded in size by a constant of the grammar.
func (c content) bounded() bool {
	return !c.unbounded && !c.vars && !c.outer && len(c.rec) == 0 && len(c.acc) == 0
}

func (c *content) add(d content) {
	c.unbounded = c.unbounded || d.unbounded
	c.vars = c.vars || d.vars
	c.outer = c.outer || d.outer
	for n, v := range d.rec {
		if c.rec == nil {
			c.rec = map[string]int{}
		}
		c.rec[n] += v
	}
	for n, v := range d.acc {
		if c.acc == nil {
			c.acc = map[string]int{}
		}
		c.acc[n] += v
	}
}

// lighter reports whether c weighs less than d as a bound on a size: it has fewer counts of fold
// accumulators, then of recursion captures, then fewer of the outer, unbounded and variable parts.
// Either of two operands bounds the shorter of them, so the choice only keeps the class tighter.
func (c content) lighter(d content) bool {
	sum := func(m map[string]int) int {
		n := 0
		for _, v := range m {
			n += v
		}
		return n
	}
	b := func(x bool) int {
		if x {
			return 1
		}
		return 0
	}
	key := func(x content) [5]int {
		return [5]int{sum(x.acc), sum(x.rec), b(x.outer), b(x.unbounded), b(x.vars)}
	}
	kc, kd := key(c), key(d)
	return slices.Compare(kc[:], kd[:]) < 0
}

// compareOps are the operators that compare two strings character by character.
var compareOps = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

// scalarOps are the binary operators other than + whose value is an int or a Boolean.
var scalarOps = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
	"-": true, "*": true, "/": true, "%": true, "&&": true, "||": true}

// copyFuncs are the built-in functions that build a value in proportion to its size. list builds
// one element for each argument, which is bounded by the grammar. len examines a string value per
// character; the length of a node or a list is constant time.
var copyFuncs = map[string]bool{"text": true, "concat": true, "map": true, "filter": true, "len": true}

func (k *classifier) walkTerm(t grammar.Term, tc termCtx) {
	if t == nil {
		return
	}
	k.control.poll()
	switch t := t.(type) {
	case *grammar.TermGroup:
		k.walkTerm(t.Term, tc)
	case *grammar.Block:
		for _, s := range t.Statements {
			k.walkTerm(s, tc)
		}
	case *grammar.Bind:
		k.binds = append(k.binds, &binding{rule: tc.rule, b: t, c: tc.c})
		k.walkTerm(t.Value, tc)
	case *grammar.Member:
		k.walkTerm(t.X, tc)
	case *grammar.Cast:
		k.walkTerm(t.X, tc)
	case *grammar.Unary:
		k.walkTerm(t.X, tc)
	case *grammar.New:
		for _, f := range t.Fields {
			k.walkTerm(f.Value, tc)
		}
		for _, a := range t.Args {
			k.walkTerm(a, tc)
		}
	case *grammar.TupleLit:
		for _, x := range t.Elems {
			k.walkTerm(x, tc)
		}
	case *grammar.ListConstructor:
		for _, x := range t.Items {
			k.walkTerm(x, tc)
		}
	case *grammar.Binary:
		if t.Op == "+" && !k.isInt(t.L, tc) && !k.isInt(t.R, tc) {
			k.copyOp(t, termPosOf(t), tc)
			// The joined operands are one operation; walk the terms below them.
			for _, x := range k.joined(t, nil) {
				k.walkTerm(x, tc)
			}
			return
		}
		if compareOps[t.Op] && !k.isInt(t.L, tc) && !k.isInt(t.R, tc) {
			// Comparing two strings examines the characters of the shorter one, which is no
			// larger than either: count the operand that weighs less.
			l, r := k.sizeOf(t.L, tc, textSize), k.sizeOf(t.R, tc, textSize)
			if !l.bounded() && !r.bounded() {
				if r.lighter(l) {
					l = r
				}
				k.copied(t, termPosOf(t), tc, l)
			}
		}
		k.walkTerm(t.L, tc)
		k.walkTerm(t.R, tc)
	case *grammar.Call:
		for i, a := range t.Args {
			if lam, ok := a.(*grammar.Lambda); ok {
				k.walkTerm(lam.Body, k.lambdaCtx(t, i, lam, tc))
				continue
			}
			k.walkTerm(a, tc)
		}
		switch {
		case t.Func == "len":
			// len builds an int, but examines all of a string value.
			var size content
			for _, a := range t.Args {
				if k.mayBeString(a) {
					size.add(k.sizeOf(a, tc, textSize))
				}
			}
			if k.types == nil && !size.bounded() {
				k.stringLen = true
			}
			k.copied(t, termPosOf(t), tc, size)
		case copyFuncs[t.Func]:
			k.copyOp(t, termPosOf(t), tc)
		}
	case *grammar.Lambda:
		k.walkTerm(t.Body, tc)
	}
}

// mayBeString reports whether the value of t may be a string value rather than a node, a list or
// a tuple. Without the type of t it may.
func (k *classifier) mayBeString(t grammar.Term) bool {
	if k.types == nil {
		return true
	}
	ty, ok := k.types.Terms[t]
	return !ok || k.stringIn(ty, map[string]bool{})
}

// stringIn reports whether a value of type t may be a string value, reading the value types of
// the rules that t refers to once each. An unknown type may.
func (k *classifier) stringIn(t Ty, seen map[string]bool) bool {
	switch t := t.(type) {
	case NodeTy, ListTy, TupleTy:
		return false
	case OptTy:
		return k.stringIn(t.Elem, seen)
	case JoinTy:
		for _, a := range t.Alts {
			if k.stringIn(a, seen) {
				return true
			}
		}
		return false
	case RuleTy:
		if seen[t.Name] {
			return false
		}
		seen[t.Name] = true
		r, ok := k.types.Rules[t.Name]
		return !ok || k.stringIn(r, seen)
	case BasicTy:
		return t == TyString || t == TyTop || t == TyAny
	}
	return true
}

// lambdaCtx returns the context of the body of the lambda that is argument i of call.
func (k *classifier) lambdaCtx(call *grammar.Call, i int, lam *grammar.Lambda, tc termCtx) termCtx {
	params := map[string]param{}
	for n, p := range tc.params {
		params[n] = p
	}
	fold := call.Func == "foldl" || call.Func == "foldr"
	// An element of the list argument can hold anything that the list can.
	var elem content
	var elems []grammar.Expr
	if list := 0; !fold || len(call.Args) > 1 {
		if fold {
			list = 1
		}
		if len(call.Args) > list {
			elem = k.elemSize(call.Args[list], tc)
			elems = k.elemExprs(call.Args[list], tc)
		}
	}
	for j, n := range lam.Params {
		p := param{}
		if fold && j == 0 {
			p.acc = true
			p.int = len(call.Args) > 0 && k.isInt(call.Args[0], tc)
		} else {
			p.elem, p.elems = elem, elems
		}
		params[n] = p
	}
	tc.params, tc.lambda = params, true
	return tc
}

// elemSize describes what the size of an element of the list t can depend on: anything that the
// captures and variables that t reads can hold.
func (k *classifier) elemSize(t grammar.Term, tc termCtx) content {
	saved := tc.lambda
	tc.lambda = false
	c := k.sizeOf(&grammar.Member{X: t}, tc, textSize)
	tc.lambda = saved
	c.outer = false
	return c
}

// joined flattens a join of strings with + into its operands.
func (k *classifier) joined(t grammar.Term, acc []grammar.Term) []grammar.Term {
	switch x := t.(type) {
	case *grammar.TermGroup:
		return k.joined(x.Term, acc)
	case *grammar.Binary:
		if x.Op == "+" {
			return k.joined(x.R, k.joined(x.L, acc))
		}
	}
	return append(acc, t)
}

// isInt reports whether t is known to be an int.
func (k *classifier) isInt(t grammar.Term, tc termCtx) bool {
	switch t := t.(type) {
	case *grammar.IntLit:
		return true
	case *grammar.TermGroup:
		return k.isInt(t.Term, tc)
	case *grammar.Unary:
		return t.Op == "-"
	case *grammar.Binary:
		switch t.Op {
		case "-", "*", "/", "%":
			return true
		case "+":
			return k.isInt(t.L, tc) || k.isInt(t.R, tc)
		}
	case *grammar.Call:
		return t.Func == "len"
	case *grammar.VarRef:
		return k.varType[t.Name] == "int"
	case *grammar.CaptureRef:
		if p, ok := tc.params[t.Name]; ok && t.Param {
			return p.int
		}
	case *grammar.Cast:
		if r, ok := t.Type.(*grammar.TypeRef); ok {
			return r.Name == "int"
		}
	}
	return false
}

// sizeMode says how a copying operation measures an operand: the characters of a string or the
// elements of a list. A list operand costs its number of elements; its elements are shared.
type sizeMode uint8

const (
	textSize sizeMode = iota
	listSize
)

// sizeOf describes what the size of the value of t, measured in mode, can depend on.
func (k *classifier) sizeOf(t grammar.Term, tc termCtx, mode sizeMode) content {
	var c content
	switch t := t.(type) {
	case nil, *grammar.IntLit, *grammar.BoolLit, *grammar.NilLit, *grammar.StringLit, *grammar.Unary, *grammar.Lambda,
		*grammar.New, *grammar.TupleLit, *grammar.ListConstructor:
		// Numbers, Booleans and structs are not strings or lists; a list literal has as many
		// elements as it has items.
	case *grammar.TermGroup:
		return k.sizeOf(t.Term, tc, mode)
	case *grammar.Cast:
		return k.sizeOf(t.X, tc, mode)
	case *grammar.VarRef:
		c.vars = true
	case *grammar.CaptureRef:
		if p, ok := tc.params[t.Name]; ok && (t.Param || tc.lambda) {
			if p.acc {
				c.acc = map[string]int{t.Name: 1}
			}
			c.add(p.elem)
			c.unbounded = true
			return c
		}
		exprs := k.captures[tc.rule][t.Name]
		if len(exprs) == 0 {
			c.unbounded = true
		}
		for _, e := range exprs {
			c.add(k.exprSize(e, tc, mode, "$"+t.Name))
		}
		c.outer = tc.lambda && c.unbounded
	case *grammar.IndexRef:
		c = k.deepSize(k.byName[tc.rule].Expr, tc, fmt.Sprintf("$%d", t.Index))
		c.outer = tc.lambda && c.unbounded
	case *grammar.Member:
		exprs, param, ok := k.termExprs(t, tc)
		if !ok {
			return k.sizeOfAny(t, tc)
		}
		name := "$" + strings.TrimPrefix(t.Name, "$")
		if len(exprs) == 0 {
			c.unbounded = true
		}
		for _, e := range exprs {
			c.add(k.exprSize(e, tc, mode, name))
		}
		c.outer = tc.lambda && !param && c.unbounded
	case *grammar.Binary:
		switch {
		case t.Op == "+":
			if !k.isInt(t.L, tc) && !k.isInt(t.R, tc) {
				c.add(k.sizeOf(t.L, tc, textSize))
				c.add(k.sizeOf(t.R, tc, textSize))
			}
		case scalarOps[t.Op]:
			// An int or a Boolean.
		default:
			// An element of a list, as x[i], can hold anything that the list can.
			return k.sizeOfAny(t, tc)
		}
	case *grammar.Call:
		switch t.Func {
		case "len", "list":
			// An int, and a list with one element for each argument.
		case "text":
			for _, a := range t.Args {
				c.add(k.sizeOf(a, tc, textSize))
			}
		case "concat":
			for _, a := range t.Args {
				c.add(k.sizeOf(a, tc, listSize))
			}
		case "map", "filter":
			if len(t.Args) > 0 {
				c.add(k.sizeOf(t.Args[0], tc, listSize))
			}
		case "foldl", "foldr":
			if len(t.Args) > 0 {
				c.add(k.sizeOf(t.Args[0], tc, mode))
			}
			for i, a := range t.Args {
				if lam, ok := a.(*grammar.Lambda); ok {
					c.add(k.sizeOf(lam.Body, k.lambdaCtx(t, i, lam, tc), mode))
				}
			}
		default:
			c.unbounded = true
		}
	default:
		return k.sizeOfAny(t, tc)
	}
	return c
}

// sizeOfAny describes the size of the value of t without looking at how it is built: anything
// that the captures and variables it reads can hold.
func (k *classifier) sizeOfAny(t grammar.Term, tc termCtx) content {
	var c content
	outer := false
	{
		WalkTerm(t, func(x grammar.Term) {
			switch x := x.(type) {
			case *grammar.CaptureRef:
				if p, ok := tc.params[x.Name]; ok && (x.Param || tc.lambda) {
					if p.acc {
						c.add(content{acc: map[string]int{x.Name: 1}})
					}
					c.add(p.elem)
					return
				}
				outer = true
				for _, e := range k.captures[tc.rule][x.Name] {
					c.add(k.deepSize(e, tc, "$"+x.Name))
				}
			case *grammar.IndexRef:
				outer = true
				c.add(k.deepSize(k.byName[tc.rule].Expr, tc, fmt.Sprintf("$%d", x.Index)))
			case *grammar.VarRef:
				c.vars = true
			}
		})
		c.unbounded = true
		c.outer = tc.lambda && outer
	}
	return c
}

// termExprs returns the captured expressions whose values t can be: a capture, a member $y of
// such a value, which is the value of a capture y inside it, or an element of a list whose
// elements are known. param reports a value that comes from a lambda parameter.
func (k *classifier) termExprs(t grammar.Term, tc termCtx) (exprs []grammar.Expr, param, ok bool) {
	switch t := t.(type) {
	case *grammar.TermGroup:
		return k.termExprs(t.Term, tc)
	case *grammar.CaptureRef:
		if p, isParam := tc.params[t.Name]; isParam && (t.Param || tc.lambda) {
			return p.elems, true, p.elems != nil && !p.acc
		}
		exprs := k.captures[tc.rule][t.Name]
		return exprs, false, len(exprs) > 0
	case *grammar.Member:
		if !strings.HasPrefix(t.Name, "$") {
			return nil, false, false
		}
		inner, param, ok := k.termExprs(t.X, tc)
		if !ok {
			return nil, false, false
		}
		name := strings.TrimPrefix(t.Name, "$")
		for _, e := range inner {
			// The node of a call of a rule without an action holds the captures of its body.
			if r, isRef := unwrapValue(e).(*grammar.Ref); isRef {
				if d := k.byName[r.Name]; d != nil && d.Action == nil && !isOperators(d.Expr) {
					e = d.Expr
				}
			}
			found := false
			WalkExpr(e, func(x grammar.Expr) {
				if cp, isCap := x.(*grammar.Capture); isCap && cp.Name == name {
					exprs = append(exprs, cp.Expr)
					found = true
				}
			})
			if !found {
				// The value can be one that the expression does not build itself, such as the
				// value of a rule that it calls.
				return nil, false, false
			}
		}
		return exprs, param, true
	}
	return nil, false, false
}

// elemExprs returns the expressions whose values are the elements of the list t, or nil when
// they are not known: t is the value of repetitions.
func (k *classifier) elemExprs(t grammar.Term, tc termCtx) []grammar.Expr {
	exprs, _, ok := k.termExprs(t, tc)
	if !ok {
		return nil
	}
	var out []grammar.Expr
	for _, e := range exprs {
		e = unwrapValue(e)
		if o, isOpt := e.(*grammar.Optional); isOpt {
			e = unwrapValue(o.Expr)
		}
		r, isRep := e.(*grammar.Repeat)
		if !isRep {
			return nil
		}
		out = append(out, r.Expr)
	}
	return out
}

// unwrapValue removes the groups, captures, discards and atomic expressions around e, which do not
// change its value.
func unwrapValue(e grammar.Expr) grammar.Expr {
	for {
		switch x := e.(type) {
		case *grammar.Group:
			e = x.Expr
		case *grammar.Capture:
			e = x.Expr
		case *grammar.Atomic:
			e = x.Expr
		default:
			return e
		}
	}
}

// exprSize describes what the size of the value of the captured expression e, measured in mode,
// can depend on. name names the capture in witnesses.
func (k *classifier) exprSize(e grammar.Expr, tc termCtx, mode sizeMode, name string) content {
	switch x := e.(type) {
	case *grammar.Group:
		return k.exprSize(x.Expr, tc, mode, name)
	case *grammar.Capture:
		return k.exprSize(x.Expr, tc, mode, name)
	case *grammar.Discard:
		return k.exprSize(x.Expr, tc, mode, name)
	case *grammar.Atomic:
		return k.exprSize(x.Expr, tc, mode, name)
	case *grammar.Optional:
		return k.exprSize(x.Expr, tc, mode, name)
	case *grammar.Choice:
		var c content
		for _, alt := range x.Alts {
			c.add(k.exprSize(alt, tc, mode, name))
		}
		return c
	case *grammar.Repeat:
		if mode == listSize {
			// A list with one element for each iteration.
			return content{unbounded: x.Max < 0}
		}
	case *grammar.Ref:
		if k.byName[x.Name] == nil {
			return content{}
		}
		c := content{unbounded: !k.bounded(e)}
		if k.onRecursion(tc.rule, x.Name) && k.includes(x.Name, mode) {
			c.rec = map[string]int{name: 1}
		}
		return c
	}
	return k.deepSize(e, tc, name)
}

// deepSize describes the size of the value of e without looking at how it is built: it can hold
// anything that e matches or calls.
func (k *classifier) deepSize(e grammar.Expr, tc termCtx, name string) content {
	c := content{unbounded: !k.bounded(e)}
	found := false
	WalkExpr(e, func(x grammar.Expr) {
		if r, ok := x.(*grammar.Ref); ok && k.byName[r.Name] != nil && k.onRecursion(tc.rule, r.Name) {
			found = true
		}
	})
	if found {
		c.rec = map[string]int{name: 1}
	}
	return c
}

// onRecursion reports whether a call of callee in rule is a call on a recursion with rule.
func (k *classifier) onRecursion(rule, callee string) bool {
	return k.recursive[rule] && k.scc[callee] == k.scc[rule]
}

// includes reports whether the size of the value of rule, measured in mode, can include the size
// of the value of a call on a recursion with it, so that the sizes of the values of the recursion
// add up. A value that passes the value of a call on, as $x does, includes what that value
// includes. A rule on a cycle of such values is assumed to include them, as is an Operators Rule.
func (k *classifier) includes(rule string, mode sizeMode) bool {
	key := sizeKey{rule, mode}
	if v, ok := k.includesMemo[key]; ok {
		return v
	}
	k.includesMemo[key] = true
	r := k.byName[rule]
	v := true
	tc := termCtx{rule: rule}
	switch {
	case isOperators(r.Expr):
	case r.Action != nil:
		v = len(k.sizeOf(r.Action, tc, mode).rec) > 0
	default:
		// The value of a rule without an action is the value of its expression.
		v = len(k.exprSize(r.Expr, tc, mode, rule).rec) > 0
	}
	k.includesMemo[key] = v
	return v
}

func isOperators(e grammar.Expr) bool {
	_, ok := e.(*grammar.OperatorsRule)
	return ok
}

type sizeKey struct {
	rule string
	mode sizeMode
}

// copyOp checks a copying operation (condition 4) and records it as a scanning source when an
// operand can be larger than a constant of the grammar.
func (k *classifier) copyOp(t grammar.Term, pos grammar.Pos, tc termCtx) {
	k.copied(t, pos, tc, k.sizeOf(t, tc, textSize))
}

// copied checks the copying operation t, which copies a value of the size all.
func (k *classifier) copied(t grammar.Term, pos grammar.Pos, tc termCtx, all content) {
	text := k.showTerm(t)
	worst := func(m map[string]int) (string, int) {
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Strings(names)
		best, count := "", 0
		for _, n := range names {
			if m[n] > count {
				best, count = n, m[n]
			}
		}
		return best, count
	}
	if n, count := worst(all.acc); count > 0 {
		class, degree := ClassPoly, 2
		if count > 1 {
			class, degree = ClassExp, 0
		}
		k.cause(class, degree, tc.rule, pos, "%s: %s at %s copies the fold accumulator %s%s",
			degreeText(class, degree), text, at(tc.rule, pos), n, times(count))
	} else if n, count := worst(all.rec); count > 0 {
		class, degree := ClassPoly, 2
		if count > 1 {
			class, degree = ClassExp, 0
		}
		k.cause(class, degree, tc.rule, pos, "%s: %s at %s copies %s, which holds a call on a recursion with rule %s%s",
			degreeText(class, degree), text, at(tc.rule, pos), n, tc.rule, times(count))
	}
	if all.outer {
		k.reason(tc.rule, pos, "%s at %s copies, for each element, a value from outside its lambda that is not bounded by a constant",
			text, at(tc.rule, pos))
	}
	if all.vars {
		k.envCause(tc.rule, pos, "%s at %s copies the value of a variable", text, at(tc.rule, pos))
	}
	if all.unbounded || len(all.rec) > 0 || len(all.acc) > 0 {
		s := &source{rule: tc.rule, pos: pos, what: "the copying operation " + text}
		k.addSource(tc.c, pos, s.what)
		if tc.pred != nil && k.predicateSource[tc.pred] == nil {
			k.predicateSource[tc.pred] = s
		}
		if tc.pred != nil {
			k.srcExprs[tc.pred] = true
		}
		if tc.holder != nil {
			k.srcExprs[tc.holder] = true
		}
	}
}

func times(n int) string {
	if n > 1 {
		return fmt.Sprintf(" %d times", n)
	}
	return ""
}

// --- Bindings ---

// termVars returns the variables that the value of t can depend on: those it reads, and those
// that the rules called in the captures it reads read.
func (k *classifier) termVars(t grammar.Term, rule string) map[string]bool {
	out := map[string]bool{}
	ruleVars := func(e grammar.Expr) {
		WalkExpr(e, func(x grammar.Expr) {
			if r, ok := x.(*grammar.Ref); ok {
				for _, v := range k.vars[r.Name] {
					out[v] = true
				}
			}
		})
	}
	WalkTerm(t, func(x grammar.Term) {
		switch x := x.(type) {
		case *grammar.VarRef:
			out[x.Name] = true
		case *grammar.CaptureRef:
			if !x.Param {
				for _, e := range k.captures[rule][x.Name] {
					ruleVars(e)
				}
			}
		case *grammar.IndexRef:
			ruleVars(k.byName[rule].Expr)
		}
	})
	return out
}

// constant reports whether t reads nothing that the input can change.
func constant(t grammar.Term) bool {
	ok := true
	WalkTerm(t, func(x grammar.Term) {
		switch x.(type) {
		case *grammar.VarRef, *grammar.CaptureRef, *grammar.IndexRef, *grammar.Member:
			ok = false
		}
	})
	return ok
}

// selfDependent reports whether the binding reads, through any chain of bindings, captures and
// rule values, its variable's own earlier value.
func (k *classifier) selfDependent(b *binding, deps map[string]map[string]bool) bool {
	seen := map[string]bool{}
	var visit func(v string) bool
	visit = func(v string) bool {
		if v == b.b.Name {
			return true
		}
		if seen[v] {
			return false
		}
		seen[v] = true
		for w := range deps[v] {
			if visit(w) {
				return true
			}
		}
		return false
	}
	for v := range k.termVars(b.b.Value, b.rule) {
		if visit(v) {
			return true
		}
	}
	return false
}

// reaches reports whether the value that the binding binds can reach a later evaluation of the
// same binding: it is in the element of a repetition or an Operators Rule, outside a negative
// lookahead, or a sequence has it in one element and a call that can reach its rule again in a
// later element. strict limits the depths to groups, options, choices, captures, discards and
// atomic expressions, which the growing-variable check requires.
func (k *classifier) reaches(b *binding, strict bool) (string, bool) {
	if strict {
		if b.c.inNot {
			return "", false
		}
		if b.c.inRepeat {
			return "the repetition around it", true
		}
	} else if (b.c.inRepeat || b.c.ops) && !b.c.inNot {
		return "the repetition around it", true
	}
	frames := b.c.seqs
	if strict {
		frames = frames[b.c.strictFrom:]
	}
	for _, f := range frames {
		for _, later := range f.seq.Items[f.index+1:] {
			if r := k.reachingCall(later, b.rule, strict); r != nil {
				return "the call " + r.Name + " at " + at(b.rule, exprPos(r)), true
			}
		}
	}
	return "", false
}

// reachingCall returns a call in e that can reach rule again.
func (k *classifier) reachingCall(e grammar.Expr, rule string, strict bool) *grammar.Ref {
	var found *grammar.Ref
	var visit func(e grammar.Expr)
	visit = func(e grammar.Expr) {
		if found != nil || e == nil {
			return
		}
		switch x := e.(type) {
		case *grammar.Ref:
			if k.byName[x.Name] != nil && k.reachable(x.Name)[rule] {
				found = x
			}
		case *grammar.Group:
			visit(x.Expr)
		case *grammar.Optional:
			visit(x.Expr)
		case *grammar.Capture:
			visit(x.Expr)
		case *grammar.Discard:
			visit(x.Expr)
		case *grammar.Atomic:
			visit(x.Expr)
		case *grammar.Seq:
			for _, it := range x.Items {
				visit(it)
			}
		case *grammar.Choice:
			for _, alt := range x.Alts {
				visit(alt)
			}
		default:
			if !strict {
				WalkExpr(e, func(y grammar.Expr) {
					if r, ok := y.(*grammar.Ref); ok && found == nil && k.byName[r.Name] != nil && k.reachable(r.Name)[rule] {
						found = r
					}
				})
			}
		}
	}
	visit(e)
	return found
}

// checkValues checks condition 1 and records the bindings that make the class linear given an
// environment bound.
func (k *classifier) checkValues() {
	deps := map[string]map[string]bool{}
	for _, b := range k.binds {
		if deps[b.b.Name] == nil {
			deps[b.b.Name] = map[string]bool{}
		}
		for v := range k.termVars(b.b.Value, b.rule) {
			deps[b.b.Name][v] = true
		}
	}
	for _, b := range k.binds {
		pos := termPosOf(b.b)
		text := k.showTerm(b.b)
		if !constant(b.b.Value) {
			k.envCause(b.rule, pos, "the binding %s at %s binds a value that the input determines", text, at(b.rule, pos))
		}
		if !k.selfDependent(b, deps) {
			continue
		}
		if via, ok := k.reaches(b, false); ok {
			k.cause(ClassExp, 0, b.rule, pos, "%s: the binding %s at %s reads the earlier value of %s, and %s evaluates the binding again with the value it binds",
				degreeText(ClassExp, 0), text, at(b.rule, pos), b.b.Name, via)
		}
	}
}

// growingBindings returns the bindings that the growing-variable check reports: a binding of a
// string variable whose value joins the variable's own value at least twice, when the variable
// can hold a non-empty value there and the value reaches a later evaluation of the binding.
func (k *classifier) growingBindings() []GrowingBinding {
	var out []GrowingBinding
	for _, b := range k.binds {
		v := b.b.Name
		if k.varType[v] != "string" {
			continue
		}
		n := 0
		for _, x := range k.joined(b.b.Value, nil) {
			if r, ok := ungroupTerm(x).(*grammar.VarRef); ok && r.Name == v {
				n++
			}
		}
		if n < 2 {
			continue
		}
		if _, ok := k.reaches(b, true); !ok || !k.canHoldNonEmpty(b) {
			continue
		}
		out = append(out, GrowingBinding{Rule: b.rule, Pos: termPosOf(b.b), Var: v, Text: grammar.FormatTerm(b.b)})
	}
	return out
}

// canHoldNonEmpty reports whether the variable of the binding can hold a non-empty value at it:
// its initial value is not empty, or a binding of a value that can be non-empty precedes it in
// its rule body, or in a rule that calls its rule, directly or indirectly, before the call.
func (k *classifier) canHoldNonEmpty(b *binding) bool {
	v := b.b.Name
	for _, d := range k.g.Vars() {
		if d.Name == v {
			if s, ok := d.Value.(*grammar.StringLit); ok && s.Value != "" {
				return true
			}
		}
	}
	before := func(frames []seqFrame) bool {
		for _, f := range frames {
			for _, it := range f.seq.Items[:f.index] {
				if k.bindsNonEmpty(it, v) {
					return true
				}
			}
		}
		return false
	}
	if before(b.c.seqs) {
		return true
	}
	for _, s := range k.sites {
		if k.reach[s.caller] && k.reachable(s.callee)[b.rule] && before(s.seqs) {
			return true
		}
	}
	return false
}

// bindsNonEmpty reports whether e has a binding of v to a value that can be non-empty.
func (k *classifier) bindsNonEmpty(e grammar.Expr, v string) bool {
	found := false
	WalkExpr(e, func(x grammar.Expr) {
		if p, ok := x.(*grammar.Predicate); ok {
			WalkTerm(p.Term, func(t grammar.Term) {
				if bd, ok := t.(*grammar.Bind); ok && bd.Name == v && k.canBeNonEmpty(bd.Value, v) {
					found = true
				}
			})
		}
	})
	return found
}

// canBeNonEmpty reports whether t can be a non-empty string when v is empty.
func (k *classifier) canBeNonEmpty(t grammar.Term, v string) bool {
	for _, x := range k.joined(t, nil) {
		switch x := ungroupTerm(x).(type) {
		case *grammar.StringLit:
			if x.Value != "" {
				return true
			}
		case *grammar.VarRef:
			if x.Name != v {
				return true
			}
		default:
			return true
		}
	}
	return false
}
