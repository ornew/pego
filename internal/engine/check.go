package engine

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ornew/pego/grammar"
)

// maxInferredType bounds further growth of a changing recursive inferred type.
// Finite acyclic types and stable oversized recursive types are not widened.
const maxInferredType = 2048

// checker infers and checks the types of a grammar.
//
// The types of rules without a declared type are found by repeating inference until no rule's
// type changes (the least fixed point). The grammar is then checked once more, reporting errors.
type checker struct {
	control *compileControl
	prog    *Program
	errs    *ErrorList
	report  bool
	// reportUnsupported reports type syntax that is not supported yet even while report is
	// false: without type checking such types would silently accept every value.
	reportUnsupported bool
	// sawUnsupported records that a resolved type used syntax that is not supported yet, whether
	// or not it was reported here, so that checks of the type are not reported on top.
	sawUnsupported bool
	// ruleTypes holds the type of each rule (declared, or being inferred).
	ruleTypes map[string]ty
	declared  map[string]bool
	// resolving holds the type aliases being resolved (for cycle detection).
	resolving map[string]bool
	rule      *grammar.RuleDef
	// streams holds the element type of the #stream repetition of each rule that has one.
	streams map[string]ty
	// checks holds the values that need a run-time check (typeInfo.checks).
	checks *checkInfo
}

func (k *checker) errorf(pos grammar.Pos, format string, args ...any) {
	if !k.report {
		return
	}
	if !pos.IsValid() && k.rule != nil {
		pos = k.rule.Diagnostic()
	}
	*k.errs = append(*k.errs, &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// dropRepeatedErrors removes the errors after the first from that repeat an earlier one at the same
// position. A type is resolved wherever it is used, so an undefined name inside an alias would be
// reported once per use of the alias.
func dropRepeatedErrors(errs *ErrorList, from int) {
	type key struct {
		pos grammar.Pos
		msg string
	}
	seen := map[key]bool{}
	kept := (*errs)[:from]
	for _, e := range (*errs)[from:] {
		k := key{e.Pos, e.Msg}
		if !seen[k] {
			seen[k] = true
			kept = append(kept, e)
		}
	}
	*errs = kept
}

// checkTypes checks the types of the whole grammar.
func checkTypes(prog *Program, errs *ErrorList) {
	checkTypesControlled(prog, errs, nil)
}

func checkTypesControlled(prog *Program, errs *ErrorList, control *compileControl) {
	defer dropRepeatedErrors(errs, len(*errs))
	k := &checker{control: control, prog: prog, errs: errs, ruleTypes: map[string]ty{}, declared: map[string]bool{}, resolving: map[string]bool{}, streams: map[string]ty{}, checks: &checkInfo{fields: map[*grammar.FieldInit]ty{}, results: map[any]ty{}}}

	// Check references in type definitions.
	k.report = true
	for _, td := range control.types(prog.Grammar) {
		control.poll()
		switch spec := td.Spec.(type) {
		case *grammar.StructSpec:
			seen := map[string]bool{}
			for _, f := range spec.Fields {
				control.poll()
				if seen[f.Name] {
					k.errorf(f.Diagnostic(), "duplicate field %s in %s", f.Name, td.Name)
				}
				if !startsUpper(f.Name) {
					k.errorf(f.Diagnostic(), "field %s of %s must start with an uppercase letter", f.Name, td.Name)
				}
				seen[f.Name] = true
				k.resolveLocation(f.Type, f.Diagnostic(), "field "+f.Name+" of "+td.Name)
			}
		case *grammar.AliasSpec:
			k.resolveNamed(td.Name, td.Diagnostic())
		}
	}

	rules := control.rules(prog.Grammar)
	for _, rd := range rules {
		control.poll()
		if rd.Type != nil {
			t := k.resolveLocation(rd.Type, rd.Diagnostic(), "rule "+rd.Name)
			if !isNodeOrNil(t) {
				k.errorf(rd.Diagnostic(), "rule %s must produce a node, but its type is %s", rd.Name, t)
				t = tyAny
			}
			k.ruleTypes[rd.Name] = t
			k.declared[rd.Name] = true
		} else {
			k.ruleTypes[rd.Name] = tyNever
		}
	}

	// Inference (errors are not reported).
	k.report = false
	k.inferTypes(rules)

	// Checking (errors are reported).
	k.report = true
	for _, rd := range rules {
		control.poll()
		k.ruleType(rd)
	}

	// Keep the types for code generation (typedGo).
	k.report = false
	info := &typeInfo{rules: k.ruleTypes, fields: map[string][]typedField{}, aliases: map[string]ty{}, streams: k.streams, checks: k.checks}
	for _, td := range control.types(prog.Grammar) {
		control.poll()
		switch spec := td.Spec.(type) {
		case *grammar.StructSpec:
			for _, f := range spec.Fields {
				control.poll()
				info.fields[td.Name] = append(info.fields[td.Name], typedField{f.Name, k.resolve(f.Type)})
			}
		case *grammar.AliasSpec:
			info.aliases[td.Name] = k.resolveNamed(td.Name, td.Diagnostic())
		}
	}
	prog.typed = info
}

// typeInfo holds the types found by the type checker, for code generation.
type typeInfo struct {
	rules map[string]ty // the type of each rule
	// values holds the type of each rule's value in the value model (Program.valueTypes).
	values     map[string]ty
	valueErrs  ErrorList // the errors of actions in the value model
	valuesOnce sync.Once
	fields     map[string][]typedField // the fields of each struct type, in declaration order
	aliases    map[string]ty           // the type each alias stands for
	streams    map[string]ty           // the element type of the #stream repetition of each rule with one
	// checks holds the declared types that values must be checked against at run time: those
	// not statically known to fit (Program.fieldCheckType, Program.resultCheckType).
	checks *checkInfo
}

// RuleType returns the type of the rule name, declared or inferred, as written in a grammar (for
// example "[]Expr | *Match"). It reports false if the program has no such rule or its types were
// not checked (Options.NoTypeCheck, or a program loaded from a compiled grammar).
func (p *Program) RuleType(name string) (string, bool) {
	if p.typed == nil {
		return "", false
	}
	t, ok := p.typed.rules[name]
	if !ok {
		return "", false
	}
	return t.String(), true
}

type typedField struct {
	name string
	t    ty
}

// resolve turns a type expression into a type.
func (k *checker) resolve(t grammar.TypeExpr) ty {
	k.control.poll()
	switch t := t.(type) {
	case *grammar.TypeRef:
		return k.resolveNamed(t.Name, t.Diagnostic())
	case *grammar.ListType:
		// A constructor applied to _|_ is _|_.
		if e := k.resolve(t.Elem); e != tyNever {
			return listTy{e}
		}
		return tyNever
	case *grammar.OptionalType:
		if e := k.resolve(t.Elem); e != tyNever {
			return optional(e)
		}
		return tyNever
	case *grammar.UnionType:
		ts := make([]ty, len(t.Types))
		for i, x := range t.Types {
			ts[i] = k.resolve(x)
		}
		return union(ts...)
	case *grammar.TupleType:
		// Record 060 types: reserved syntax until values carry them.
		k.unsupported(t, t.Diagnostic(), "tuple types are not supported yet")
		return tyAny
	case *grammar.StructType:
		k.unsupported(t, t.Diagnostic(), "unnamed struct types are not supported yet; define one with type T = {...}")
		return tyAny
	case *grammar.TypeApp:
		if t.Name == "node" {
			k.unsupported(t, t.Diagnostic(), "type arguments of node are not supported yet")
		} else if k.defined(t.Name) {
			k.resolveNamed(t.Name, t.Diagnostic()) // reports an alias that refers to itself
			k.unsupported(t, t.Diagnostic(), "type %s takes no type arguments", t.Name)
		} else {
			k.unsupported(t, t.Diagnostic(), "undefined type %s", t.Name)
		}
		return tyAny
	case *grammar.IntersectionType:
		m := ty(tyTop) // the meet of no types
		for _, x := range t.Types {
			m = meet(m, k.resolve(x))
		}
		return m
	}
	return tyAny
}

// unsupported reports type syntax t that is not supported yet (reportUnsupported), once.
func (k *checker) unsupported(t grammar.TypeExpr, pos grammar.Pos, format string, args ...any) {
	k.sawUnsupported = true
	if !k.report && !k.reportUnsupported || k.prog.unsupportedSeen[t] {
		return
	}
	if k.prog.unsupportedSeen == nil {
		k.prog.unsupportedSeen = map[grammar.TypeExpr]bool{}
	}
	k.prog.unsupportedSeen[t] = true
	report := k.report
	k.report = true
	k.errorf(pos, format, args...)
	k.report = report
}

// defined reports whether name is a built-in or declared type.
func (k *checker) defined(name string) bool {
	if _, ok := k.prog.types[name]; ok {
		return true
	}
	r := &checker{prog: k.prog, resolving: map[string]bool{}, errs: &ErrorList{}, report: true}
	r.resolveNamed(name, grammar.Pos{})
	return len(*r.errs) == 0
}

// resolveLocation resolves the type of a location: a field, a rule, a list element or a cast.
// No value has the type _|_, so a location of that type is an error.
func (k *checker) resolveLocation(t grammar.TypeExpr, pos grammar.Pos, what string) ty {
	r := k.resolve(t)
	if r == tyNever {
		k.errorf(pos, "%s has type _|_, which has no values", what)
		return tyAny
	}
	return r
}

// checkBottomLocations reports the fields and rules of type _|_, and type syntax that is not
// supported yet, when types are not checked otherwise (Options.NoTypeCheck). No value fits a
// location of type _|_, while the run-time checks treat a type they cannot describe as one every
// value fits.
func checkBottomLocations(prog *Program, errs *ErrorList, control *compileControl) {
	defer dropRepeatedErrors(errs, len(*errs))
	k := &checker{control: control, prog: prog, errs: errs, resolving: map[string]bool{}, reportUnsupported: true}
	located := func(t grammar.TypeExpr, pos grammar.Pos, what string) {
		k.report = false // other type errors stay unreported, as without this check
		r := k.resolve(t)
		k.report = true
		if r == tyNever {
			k.errorf(pos, "%s has type _|_, which has no values", what)
		}
	}
	for _, td := range control.types(prog.Grammar) {
		switch spec := td.Spec.(type) {
		case *grammar.StructSpec:
			for _, f := range spec.Fields {
				located(f.Type, f.Diagnostic(), "field "+f.Name+" of "+td.Name)
			}
		case *grammar.AliasSpec:
			k.resolve(spec.Type) // an alias is not a location, but may use unsupported syntax
		}
	}
	for _, rd := range control.rules(prog.Grammar) {
		if rd.Type != nil {
			located(rd.Type, rd.Diagnostic(), "rule "+rd.Name)
		}
	}
}

// concreteType resolves aliases and normalized singleton unions to their node type.
// It does not report errors; validation still uses the type expression as written.
func (prog *Program) concreteType(t grammar.TypeExpr) (string, grammar.TypeSpec) {
	if ref, ok := t.(*grammar.TypeRef); ok {
		switch spec := prog.types[ref.Name].(type) {
		case *grammar.StructSpec:
			return ref.Name, spec
		case *grammar.TerminalSpec:
			return ref.Name, spec
		}
	}
	k := &checker{prog: prog, resolving: map[string]bool{}}
	if n, ok := k.resolve(t).(namedTy); ok && (n.kind == 's' || n.kind == 't') {
		return n.name, prog.types[n.name]
	}
	return "", nil
}

func (prog *Program) structType(name string) (string, *grammar.StructSpec) {
	switch spec := prog.types[name].(type) {
	case *grammar.StructSpec:
		return name, spec
	case *grammar.AliasSpec:
		// Resolve only aliases; ordinary constructors need no temporary type expression.
	default:
		return "", nil
	}
	canonical, spec := prog.concreteType(&grammar.TypeRef{Name: name})
	st, _ := spec.(*grammar.StructSpec)
	return canonical, st
}

func (prog *Program) terminalConstructorType(name string) string {
	canonical, spec := prog.concreteType(&grammar.TypeRef{Name: name})
	if _, ok := spec.(*grammar.TerminalSpec); ok {
		return canonical
	}
	return ""
}

// nodeListElement also rejects primitive elements hidden in nested list types.
func nodeListElement(t ty) bool {
	switch t := t.(type) {
	case listTy:
		return nodeListElement(t.elem)
	case optTy:
		return nodeListElement(t.elem)
	case unionTy:
		for _, alt := range t.alts {
			if !nodeListElement(alt) {
				return false
			}
		}
		return true
	case basicTy:
		return t != tyAny && isNode(t)
	default:
		return isNode(t)
	}
}

// unknownListElement reports any that would hide a required concrete element
// type. An abstract node destination does not constrain a known node's children.
func unknownListElement(src, dst ty) bool {
	if dst == tyAny {
		return false
	}
	if src == tyAny {
		return true
	}
	if d, ok := dst.(optTy); ok {
		if s, ok := src.(optTy); ok {
			src = s.elem
		}
		return unknownListElement(src, d.elem)
	}
	if s, ok := src.(unionTy); ok {
		for _, alt := range s.alts {
			if unknownListElement(alt, dst) {
				return true
			}
		}
		return false
	}
	if d, ok := dst.(unionTy); ok {
		for _, alt := range d.alts {
			if assignable(src, alt) && !unknownListElement(src, alt) {
				return false
			}
		}
		for _, alt := range d.alts {
			if unknownListElement(src, alt) {
				return true
			}
		}
		return false
	}
	if s, ok := src.(listTy); ok {
		if d, ok := dst.(listTy); ok {
			return unknownListElement(s.elem, d.elem)
		}
	}
	return false
}

func (k *checker) resolveNamed(name string, pos grammar.Pos) ty {
	k.control.poll()
	switch name {
	case "int":
		return tyInt
	case "string":
		return tyString
	case "bool":
		return tyBool
	case "node":
		return tyNode
	case "terminal":
		return tyTerminal
	case "any":
		return tyAny
	case "_":
		return tyTop
	case "_|_":
		return tyNever
	case TypeMatch, TypeSeq, TypeList, TypeOperator, TypeError, TypeMissing:
		return namedTy{name, 'c'}
	}
	switch spec := k.prog.types[name].(type) {
	case *grammar.StructSpec:
		return namedTy{name, 's'}
	case *grammar.TerminalSpec:
		return namedTy{name, 't'}
	case *grammar.AliasSpec:
		if k.resolving[name] {
			k.errorf(pos, "type %s refers to itself", name)
			return tyAny
		}
		k.resolving[name] = true
		defer delete(k.resolving, name)
		return k.resolve(spec.Type)
	}
	k.errorf(pos, "undefined type %s", name)
	return tyAny
}

// capInfo is the type of a capture in a scope. If always is false, the capture may not match
// (it may be nil).
type capInfo struct {
	t      ty
	always bool
}

type caps map[string]capInfo

func (c caps) typeOf(name string) (ty, bool) {
	ci, ok := c[name]
	if !ok {
		return nil, false
	}
	if ci.always {
		return ci.t, true
	}
	return optional(ci.t), true
}

// record returns the type of a node that holds the captures.
func (c caps) record() recordTy {
	r := recordTy{fields: map[string]ty{}}
	for n := range c {
		r.fields[n], _ = c.typeOf(n)
	}
	return r
}

// scopeCheck is the checking state of one scope (a rule body, a repetition element, or a OperatorsRule
// line).
type scopeCheck struct {
	caps  caps
	preds []grammar.Term // predicates checked after the scope has been examined
}

// expr returns the type of the expression's value and records captures in sc. It returns nil for
// an expression without a value.
func (k *checker) expr(e grammar.Expr, sc *scopeCheck) ty {
	k.control.poll()
	switch e := e.(type) {
	case *grammar.Group:
		return k.expr(e.Expr, sc)
	case *grammar.Literal, *grammar.CharClass, *grammar.Any, *grammar.Top, *grammar.Atomic:
		return tyMatch
	case *grammar.Ref:
		if t, ok := k.ruleTypes[e.Name]; ok {
			return t
		}
		return tyAny
	case *grammar.Seq:
		for _, it := range e.Items {
			k.expr(it, sc)
		}
		return tySeq
	case *grammar.Choice:
		var ts []ty
		var all []caps
		for _, alt := range e.Alts {
			sub := &scopeCheck{caps: caps{}}
			ts = append(ts, k.expr(alt, sub))
			all = append(all, sub.caps)
			sc.preds = append(sc.preds, sub.preds...)
		}
		// Only names captured in every alternative always have a value.
		for _, c := range all {
			for n, ci := range c {
				inAll := true
				for _, o := range all {
					if oc, ok := o[n]; !ok || !oc.always {
						inAll = false
					}
				}
				k.addCapture(sc, n, capInfo{t: ci.t, always: inAll})
			}
		}
		return union(ts...)
	case *grammar.Repeat:
		inner := &scopeCheck{caps: caps{}}
		t := k.expr(e.Expr, inner)
		k.checkPreds(inner)
		if len(inner.caps) > 0 {
			t = inner.caps.record()
		}
		if t == nil {
			t = tyNever
		}
		return listTy{t}
	case *grammar.Optional:
		sub := &scopeCheck{caps: caps{}}
		t := k.expr(e.Expr, sub)
		for n, ci := range sub.caps {
			k.addCapture(sc, n, capInfo{t: ci.t, always: false})
		}
		sc.preds = append(sc.preds, sub.preds...)
		if t == nil {
			return tyNil
		}
		return optional(t)
	case *grammar.And:
		k.expr(e.Expr, sc)
		return nil
	case *grammar.Not, *grammar.Discard, *grammar.Cut, *grammar.Bottom, *grammar.BeginInput,
		*grammar.EndInput, *grammar.BeginLine, *grammar.EndLine:
		return nil
	case *grammar.Capture:
		t := k.expr(e.Expr, sc)
		if t == nil {
			t = tyAny
		}
		k.addCapture(sc, e.Name, capInfo{t: t, always: true})
		return t
	case *grammar.Predicate:
		sc.preds = append(sc.preds, e.Term)
		return nil
	case *grammar.Attributed:
		return k.attributed(e, sc)
	}
	return tyAny
}

func (k *checker) addCapture(sc *scopeCheck, name string, ci capInfo) {
	if old, ok := sc.caps[name]; ok {
		t := old.t
		// Union normalizes top-level optional and union types even when
		// both operands are equal. Other equal types can reuse the first
		// value without formatting every nested record field.
		switch t.(type) {
		case basicTy, namedTy, listTy, recordTy:
			if !sameTypeStructure(t, ci.t) {
				t = union(t, ci.t)
			}
		default:
			t = union(t, ci.t)
		}
		ci = capInfo{t: t, always: old.always || ci.always}
	}
	sc.caps[name] = ci
}

func (k *checker) checkPreds(sc *scopeCheck) {
	env := &termEnv{caps: sc.caps}
	for _, p := range sc.preds {
		if a, ok := p.(*grammar.Assign); ok {
			t := k.term(a.Value, env)
			if !assignable(t, union(tyInt, tyString, tyBool)) {
				k.errorf(a.Diagnostic(), "variable %s must be int, string, or bool, got %s", a.Name, t)
			}
			continue
		}
		k.term(p, env)
	}
}

// ruleType computes the type of the rule's value (and reports errors if report is true).
func (k *checker) ruleType(rd *grammar.RuleDef) ty {
	k.control.poll()
	k.rule = rd
	declared := k.ruleTypes[rd.Name]
	if !k.declared[rd.Name] {
		declared = nil
	}
	if pr, ok := rd.Expr.(*grammar.OperatorsRule); ok {
		return k.operatorsRuleType(rd, pr, declared)
	}
	sc := &scopeCheck{caps: caps{}}
	bodyT := k.expr(rd.Expr, sc)
	k.checkPreds(sc)
	// Positional items only feed the rule action's $N references. The body
	// pass above already reports every item's errors and records its checks,
	// so a rule without an action skips typing the items a second time.
	var items []ty
	if seq, ok := rd.Expr.(*grammar.Seq); ok && rd.Action != nil {
		for _, it := range seq.Items {
			if visible(it) {
				items = append(items, k.expr(it, &scopeCheck{caps: caps{}}))
			}
		}
	} else if bodyT != nil {
		items = []ty{bodyT}
	}
	var t ty
	if rd.Action != nil {
		t = k.term(rd.Action, &termEnv{caps: sc.caps, items: items, allowIndex: true})
		if !isNodeOrNil(t) {
			k.errorf(termPos(rd.Action, rd.Diagnostic()), "action of %s must produce a node, got %s", rd.Name, t)
			t = tyAny
		}
	} else {
		if bodyT == nil {
			bodyT = tyNil
		}
		t = bodyT
		if len(sc.caps) > 0 {
			t = sc.caps.record()
		}
		if declared != nil {
			if n, ok := declared.(namedTy); ok && n.kind == 't' {
				return declared // conversion to a terminal type
			}
		}
	}
	if declared != nil {
		if !assignable(t, declared) {
			k.errorf(rd.Diagnostic(), "rule %s produces %s, which is not assignable to %s", rd.Name, t, declared)
		} else {
			k.checkResult(rd, t, declared)
		}
		return declared
	}
	return t
}

// checkResult records that the value of type t that a rule or Operators Rule line (key) produces
// needs a run-time check against declared, the rule's declared type, when t is not known to fit.
func (k *checker) checkResult(key any, t, declared ty) {
	if k.report && k.checks != nil && unknownListElement(t, declared) && newFieldType(declared) != nil {
		k.checks.results[key] = declared
	}
}

func (k *checker) operatorsRuleType(rd *grammar.RuleDef, pr *grammar.OperatorsRule, declared ty) ty {
	self := k.ruleTypes[rd.Name]
	var ts []ty
	var keys []any
	if pr.Skip != nil {
		k.expr(pr.Skip, &scopeCheck{caps: caps{}})
	}
	for _, o := range pr.Operands {
		ts = append(ts, k.lineType(o.Expr, o.Action, nil, rd.Diagnostic()))
		keys = append(keys, o)
	}
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			keys = append(keys, op)
			if op.Action == nil {
				ts = append(ts, tyOperator)
				k.lineType(op.Expr, nil, nil, op.Diagnostic())
				continue
			}
			locals := map[string]ty{"op": tyMatch}
			if op.Kind != grammar.Prefix {
				locals["lhs"] = self
			}
			if op.Kind != grammar.Postfix {
				locals["rhs"] = self
			}
			ts = append(ts, k.lineType(op.Expr, op.Action, locals, op.Diagnostic()))
		}
	}
	if declared != nil {
		// Without an action, a rule of a terminal type converts its value, so its lines need no check.
		n, isNamed := declared.(namedTy)
		converts := rd.Action == nil && isNamed && n.kind == 't'
		for i, t := range ts {
			if !assignable(t, declared) {
				if t == tyOperator {
					k.errorf(rd.Diagnostic(), "operators of %s without an action produce Operator, which is not assignable to %s", rd.Name, declared)
				} else {
					k.errorf(rd.Diagnostic(), "operatorsRule %s produces %s, which is not assignable to %s", rd.Name, t, declared)
				}
			} else if !converts {
				k.checkResult(keys[i], t, declared)
			}
		}
		return declared
	}
	return union(ts...)
}

// lineType returns the value type of an Operators Rule operand line or operator line.
func (k *checker) lineType(e grammar.Expr, action grammar.Term, locals map[string]ty, pos grammar.Pos) ty {
	sc := &scopeCheck{caps: caps{}}
	t := k.expr(e, sc)
	k.checkPreds(sc)
	if action == nil {
		if len(sc.caps) > 0 {
			return sc.caps.record()
		}
		if t == nil {
			return tyNil
		}
		return t
	}
	env := &termEnv{caps: sc.caps, locals: locals}
	if locals == nil {
		// Operand lines may use $n.
		env.allowIndex = true
		if seq, ok := e.(*grammar.Seq); ok {
			for _, it := range seq.Items {
				if visible(it) {
					env.items = append(env.items, k.expr(it, &scopeCheck{caps: caps{}}))
				}
			}
		} else if t != nil {
			env.items = []ty{t}
		}
	}
	at := k.term(action, env)
	if !isNodeOrNil(at) {
		k.errorf(termPos(action, pos), "action must produce a node, got %s", at)
		return tyAny
	}
	return at
}

func (k *checker) attributed(e *grammar.Attributed, sc *scopeCheck) ty {
	control := k.control
	t := k.expr(e.Expr, sc)
	for _, a := range e.Attrs {
		control.poll()
		for _, arg := range control.expressionArgs(a) {
			k.expr(arg, &scopeCheck{caps: caps{}})
		}
		if a.Name == "recover" && t != nil {
			t = union(t, tyError)
		}
		if a.Name == "missing" && t != nil {
			t = union(t, tyMissing)
		}
		// The checking pass, which follows inference, records the final element type.
		if l, ok := t.(listTy); ok && a.Name == "stream" && k.rule != nil {
			k.streams[k.rule.Name] = l.elem
		}
	}
	return t
}

// termEnv is the environment for type checking value expressions.
type termEnv struct {
	caps       caps
	items      []ty
	allowIndex bool
	locals     map[string]ty
}

func (e *termEnv) with(names []string, ts []ty) *termEnv {
	n := *e
	n.locals = map[string]ty{}
	for k, v := range e.locals {
		n.locals[k] = v
	}
	for i, name := range names {
		n.locals[name] = ts[i]
	}
	return &n
}

func termPos(t grammar.Term, def grammar.Pos) grammar.Pos {
	switch t := t.(type) {
	case *grammar.TermGroup:
		return termPos(t.Term, def)
	case *grammar.CaptureRef:
		return t.Diagnostic()
	case *grammar.IndexRef:
		return t.Diagnostic()
	case *grammar.VarRef:
		return t.Diagnostic()
	case *grammar.Member:
		return t.Diagnostic()
	case *grammar.New:
		return t.Diagnostic()
	case *grammar.ListConstructor:
		return t.Diagnostic()
	case *grammar.TerminalConstructor:
		return t.Diagnostic()
	case *grammar.Call:
		return t.Diagnostic()
	case *grammar.Binary:
		return t.Diagnostic()
	case *grammar.Unary:
		return t.Diagnostic()
	case *grammar.Cast:
		return t.Diagnostic()
	case *grammar.Assign:
		return t.Diagnostic()
	case *grammar.Block:
		return t.Diagnostic()
	}
	return def
}

// term returns the type of the value expression.
func (k *checker) term(t grammar.Term, env *termEnv) ty {
	k.control.poll()
	switch t := t.(type) {
	case *grammar.IntLit:
		return tyInt
	case *grammar.StringLit:
		return tyString
	case *grammar.BoolLit:
		return tyBool
	case *grammar.NilLit:
		return tyNil
	case *grammar.CaptureRef:
		if lt, ok := env.locals[t.Name]; ok {
			return lt
		}
		if ct, ok := env.caps.typeOf(t.Name); ok {
			return ct
		}
		return tyAny // undefined captures were already reported during name resolution
	case *grammar.IndexRef:
		if !env.allowIndex {
			return tyAny
		}
		if t.Index == 0 {
			return listTy{union(env.items...)}
		}
		if t.Index > len(env.items) {
			k.errorf(t.Diagnostic(), "$%d is out of range: the rule has %d elements with a value", t.Index, len(env.items))
			return tyAny
		}
		return env.items[t.Index-1]
	case *grammar.VarRef:
		return tyAny
	case *grammar.Member:
		return k.member(k.term(t.X, env), t)
	case *grammar.New:
		name, st := k.prog.structType(t.Type)
		for _, fi := range t.Fields {
			vt := k.term(fi.Value, env)
			if st == nil {
				continue
			}
			if f := fieldOf(st, fi.Name); f != nil {
				ft := k.resolve(f.Type)
				if ft == tyNever {
					continue // reported with the declaration
				}
				if !assignable(vt, ft) {
					k.errorf(fi.Diagnostic(), "cannot use %s as %s in field %s of %s", vt, ft, fi.Name, t.Type)
				} else if k.report && k.checks != nil && unknownListElement(vt, ft) && newFieldType(ft) != nil {
					// Record only in the checking pass, where rule types are final. A field visited
					// more than once (as in a lambda) keeps a check any visit needs.
					k.checks.fields[fi] = ft
				}
			}
		}
		if st == nil {
			return tyAny
		}
		return namedTy{name, 's'}
	case *grammar.Call:
		return k.call(t, env)
	case *grammar.ListConstructor:
		reported, saw := len(*k.errs), k.sawUnsupported
		k.sawUnsupported = false
		elem := k.resolve(t.Elem)
		invalid := len(*k.errs) > reported || k.sawUnsupported
		k.sawUnsupported = saw || k.sawUnsupported
		if invalid {
			// The element type is in error already.
		} else if elem == tyNever {
			k.errorf(t.Diagnostic(), "the list element has type _|_, which has no values")
		} else if !nodeListElement(elem) {
			k.errorf(t.Diagnostic(), "list constructors require a node element type, got %s", elem)
		}
		for _, item := range t.Items {
			value := k.term(item, env)
			if unknownListElement(value, elem) {
				k.errorf(termPos(item, t.Diagnostic()), "cannot use statically unknown %s as %s in list constructor", value, elem)
			}
			if !isNodeOrNil(value) {
				k.errorf(termPos(item, t.Diagnostic()), "list constructor elements must be nodes, got %s", value)
			}
			if !assignable(value, elem) {
				k.errorf(termPos(item, t.Diagnostic()), "cannot use %s as %s in list constructor", value, elem)
			}
		}
		return listTy{elem}
	case *grammar.TerminalConstructor:
		for _, arg := range t.Args {
			value := k.term(arg, env)
			if !assignable(value, tyString) {
				k.errorf(termPos(arg, t.Diagnostic()), "terminal constructor requires a string, got %s", value)
			}
		}
		if name := k.prog.terminalConstructorType(t.Type); name != "" {
			return namedTy{name, 't'}
		}
		return tyAny
	case *grammar.Lambda:
		k.errorf(grammar.Pos{}, "functions can only be passed to filter, foldl, foldr, or map")
		return tyAny
	case *grammar.Cast:
		x, to := k.term(t.X, env), k.resolveLocation(t.Type, t.Diagnostic(), "the cast")
		if !overlaps(x, to) {
			k.errorf(t.Diagnostic(), "cannot cast %s to %s", x, to)
		}
		return to
	case *grammar.Unary:
		x := k.term(t.X, env)
		want := tyInt
		if t.Op == "!" {
			want = tyBool
		}
		if !assignable(x, want) {
			k.errorf(t.Diagnostic(), "invalid operand %s for unary %s", x, t.Op)
		}
		return want
	case *grammar.Binary:
		l, r := k.term(t.L, env), k.term(t.R, env)
		bad := func() ty {
			k.errorf(t.Diagnostic(), "invalid operands %s and %s for %s", l, r, t.Op)
			return tyAny
		}
		both := func(want ty) bool { return assignable(l, want) && assignable(r, want) }
		switch t.Op {
		case "[]":
			if !assignable(r, tyInt) {
				k.errorf(t.Diagnostic(), "index must be int, got %s", r)
			}
			e, ok := elemType(l)
			if !ok {
				k.errorf(t.Diagnostic(), "cannot index %s", l)
				return tyAny
			}
			return e
		case "==", "!=":
			return tyBool
		case "&&", "||":
			if !both(tyBool) {
				return bad()
			}
			return tyBool
		case "<", "<=", ">", ">=":
			if !both(tyInt) && !both(tyString) {
				return bad()
			}
			return tyBool
		case "+":
			if l == tyAny || r == tyAny {
				return tyAny
			}
			if both(tyInt) {
				return tyInt
			}
			if both(tyString) {
				return tyString
			}
			return bad()
		default:
			if !both(tyInt) {
				return bad()
			}
			return tyInt
		}
	case *grammar.Assign:
		return tyAny
	}
	return tyAny
}

// elemType returns the type of the elements of the lists of type t. A nil list has no elements,
// so an optional list is indexed like a list and nil adds no element type.
func elemType(t ty) (ty, bool) {
	switch t := t.(type) {
	case listTy:
		return t.elem, true
	case optTy:
		return elemType(t.elem)
	case namedTy:
		if t == tyList {
			return tyNode, true
		}
	case basicTy:
		if t == tyNode {
			// A node's elements are its children.
			return tyNode, true
		}
		if t == tyAny || t == tyNever {
			return t, true
		}
		if t == tyNil {
			return tyNever, true
		}
	case unionTy:
		var elems []ty
		for _, a := range t.alts {
			e, ok := elemType(a)
			if !ok {
				return nil, false
			}
			elems = append(elems, e)
		}
		return union(elems...), true
	}
	return nil, false
}

func (k *checker) member(x ty, m *grammar.Member) ty {
	switch m.Name {
	case "startPos", "endPos":
		if !isNode(x) {
			k.errorf(m.Diagnostic(), "cannot access .%s of %s", m.Name, x)
		}
		return tyInt
	case "children":
		if !isNode(x) {
			k.errorf(m.Diagnostic(), "cannot access .%s of %s", m.Name, x)
		}
		return listTy{optional(tyNode)}
	}
	if x == tyAny {
		return tyAny
	}
	if _, ok := x.(optTy); ok {
		k.errorf(m.Diagnostic(), "cannot access .%s of %s: the value may be nil", m.Name, x)
		return tyAny
	}
	if name, ok := strings.CutPrefix(m.Name, "$"); ok {
		return k.captureAccess(x, name, m)
	}
	var alts []ty
	if u, ok := x.(unionTy); ok {
		alts = u.alts
	} else {
		alts = []ty{x}
	}
	var found []ty
	for _, a := range alts {
		switch a := a.(type) {
		case namedTy:
			if a == tyMissing && (m.Name == "expected" || m.Name == "message") {
				found = append(found, tyString)
			}
			if st, ok := k.prog.types[a.name].(*grammar.StructSpec); ok {
				if f := fieldOf(st, m.Name); f != nil {
					found = append(found, k.resolve(f.Type))
				}
			}
		case recordTy:
			if ft, ok := a.fields[m.Name]; ok {
				found = append(found, ft)
			}
		case basicTy:
			if a == tyNode {
				found = append(found, tyAny)
			}
		}
	}
	if len(found) == 0 {
		k.errorf(m.Diagnostic(), "%s has no field %s", x, m.Name)
		return tyAny
	}
	if len(found) < len(alts) {
		// A field present in only some of the types is nil for the others.
		return optional(union(found...))
	}
	return union(found...)
}

// captureAccess returns the type of x.$name: every node type of x must capture name, and the
// type is the join of their capture types.
func (k *checker) captureAccess(x ty, name string, m *grammar.Member) ty {
	alts := []ty{x}
	if u, ok := x.(unionTy); ok {
		alts = u.alts
	}
	var found []ty
	for _, a := range alts {
		switch a := a.(type) {
		case recordTy:
			if ft, ok := a.fields[name]; ok {
				found = append(found, ft)
				continue
			}
		case basicTy:
			if a == tyNode || a == tyAny {
				found = append(found, tyAny)
				continue
			}
		}
		k.errorf(m.Diagnostic(), "%s has no capture %s", a, name)
		return tyAny
	}
	return union(found...)
}

// elemType returns the element type of a list (or an optional list).
func (k *checker) elemType(fn string, t ty, pos grammar.Pos) ty {
	switch t := t.(type) {
	case listTy:
		return t.elem
	case optTy:
		return k.elemType(fn, t.elem, pos)
	case unionTy:
		// A union of list types, such as []A | []B, holds elements of either type.
		elems := make([]ty, len(t.alts))
		for i, alt := range t.alts {
			elems[i] = k.elemType(fn, alt, pos)
		}
		return union(elems...)
	}
	switch t {
	case tyNever:
		return tyNever
	case tyError, tyMissing:
		// Recovery nodes have no children, so they behave as empty lists.
		return tyNever
	case tyNil:
		// Nil is an empty list.
		return tyNever
	case tyAny, tyNode:
		return tyAny
	case tyList, tySeq, tyOperator:
		return optional(tyNode)
	}
	k.errorf(pos, "%s: expected a list, got %s", fn, t)
	return tyAny
}

func (k *checker) lambda(fn string, t grammar.Term, n int, pos grammar.Pos) (*grammar.Lambda, bool) {
	l, ok := t.(*grammar.Lambda)
	if !ok || len(l.Params) != n {
		k.errorf(pos, "%s: expected a function of %d parameters", fn, n)
		return nil, false
	}
	return l, true
}

func (k *checker) call(t *grammar.Call, env *termEnv) ty {
	if n, ok := builtins[t.Func]; !ok || n >= 0 && n != len(t.Args) {
		return tyAny // already reported during name resolution
	}
	argT := func(i int) ty { return k.term(t.Args[i], env) }
	switch t.Func {
	case "len":
		a := argT(0)
		if !assignable(a, union(tyString, optional(tyNode))) {
			k.errorf(t.Diagnostic(), "len: invalid argument %s", a)
		}
		return tyInt
	case "text":
		a := argT(0)
		if !assignable(a, union(tyString, optional(tyNode))) {
			k.errorf(t.Diagnostic(), "text: invalid argument %s", a)
		}
		return tyString
	case "foldl", "foldr":
		acc := argT(0)
		elem := k.elemType(t.Func, argT(1), t.Diagnostic())
		l, ok := k.lambda(t.Func, t.Args[2], 2, t.Diagnostic())
		if !ok {
			return tyAny
		}
		// If the body's type does not fit the initial value's type, take the union and check again.
		for i := 0; i < 3; i++ {
			saved := k.report
			k.report = false
			body := k.term(l.Body, env.with(l.Params, []ty{acc, elem}))
			k.report = saved
			if assignable(body, acc) {
				break
			}
			acc = union(acc, body)
		}
		k.term(l.Body, env.with(l.Params, []ty{acc, elem}))
		return acc
	case "filter":
		elem := k.elemType("filter", argT(0), t.Diagnostic())
		l, ok := k.lambda("filter", t.Args[1], 1, t.Diagnostic())
		if !ok {
			return tyAny
		}
		body := k.term(l.Body, env.with(l.Params, []ty{elem}))
		if !assignable(body, tyBool) {
			k.errorf(t.Diagnostic(), "filter: predicate must produce a bool, got %s", body)
		}
		return listTy{elem}
	case "map":
		elem := k.elemType("map", argT(0), t.Diagnostic())
		l, ok := k.lambda("map", t.Args[1], 1, t.Diagnostic())
		if !ok {
			return tyAny
		}
		body := k.term(l.Body, env.with(l.Params, []ty{elem}))
		if !isNodeOrNil(body) {
			k.errorf(t.Diagnostic(), "map: the function must produce a node, got %s", body)
		}
		return listTy{body}
	case "list":
		var elems []ty
		for i := range t.Args {
			a := argT(i)
			if !isNode(a) && !assignable(a, optional(tyNode)) {
				k.errorf(t.Diagnostic(), "list: elements must be nodes, got %s", a)
			}
			elems = append(elems, a)
		}
		return listTy{union(elems...)}
	case "concat":
		var elems []ty
		for i := range t.Args {
			elems = append(elems, k.elemType("concat", argT(i), t.Diagnostic()))
		}
		return listTy{union(elems...)}
	}
	return tyAny
}
