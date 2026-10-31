package compile

import (
	"sync"

	"github.com/ornew/zeroparse/grammar"
)

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

// CheckTypes checks the types of the whole grammar.
func CheckTypes(prog *Program, errs *ErrorList) {
	CheckTypesControlled(prog, errs, nil)
}

// CheckTypesControlled is CheckTypes polling control for cancellation; a nil control never
// cancels.
func CheckTypesControlled(prog *Program, errs *ErrorList, control *Control) {
	defer dropRepeatedErrors(errs, len(*errs))
	k := &TypeResolver{control: control, Prog: prog, errs: errs, report: true}
	k.checkTypeDefinitions()
	rules := control.rules(prog.Grammar)
	for _, rd := range rules {
		control.poll()
		if rd.Type != nil {
			k.resolveLocation(rd.Type, rd.Diagnostic(), "rule "+rd.Name)
		}
	}

	// Check the values.
	nodeMembers := map[*grammar.Member]bool{}
	values, valueErrs := valueTypesChecked(prog, control, nodeMembers, nil)
	*errs = append(*errs, valueErrs...)

	// Keep the types for code generation (typedGo).
	info := &TypeInfo{Fields: map[string][]typedField{}, Aliases: map[string]Ty{}, NodeMembers: nodeMembers, Values: values, valueErrs: valueErrs}
	info.valuesOnce.Do(func() {})
	info.shown = shownRuleTypes(prog, rules, values, control)
	k.report = false
	for _, td := range control.types(prog.Grammar) {
		control.poll()
		switch spec := td.(type) {
		case *grammar.TypeDef:
			for _, f := range DefFields(spec) {
				control.poll()
				info.Fields[td.DeclName()] = append(info.Fields[td.DeclName()], typedField{f.Name, k.Resolve(f.Type)})
			}
		case *grammar.TypeAlias:
			info.Aliases[td.DeclName()] = JoinMembers(k.ResolveNamed(td.DeclName(), td.Diagnostic()))
		}
	}
	prog.Typed = info
}

// TypeInfo holds the types found by the type checker, for code generation.
type TypeInfo struct {
	// Values holds the type of each rule's value in the value model (Program.ValueTypes).
	Values     map[string]Ty
	valueErrs  ErrorList     // the errors of actions in the value model
	shown      map[string]Ty // the type of each rule as RuleType shows it (shownRuleTypes)
	valuesOnce sync.Once
	Fields     map[string][]typedField // the fields of each struct type, in declaration order
	Aliases    map[string]Ty           // the type each alias stands for
	// NodeMembers reports, for each member access of an action, whether it reads only node
	// members (.Start, .End, .Value) and never a struct field.
	NodeMembers map[*grammar.Member]bool
	// rec holds the types of the terms, scopes and rules of the checked program, made on first
	// use by Program.ValueRecord.
	rec     *ValueRecord
	recOnce sync.Once
}

// ValueRecord returns the types the value inference gives the parts of the type-checked program
// (valueRecord), or nil if its types were not checked. Only code generation reads them, so they
// are recorded by a second run of the inference on first use rather than on every Compile.
func (p *Program) ValueRecord() *ValueRecord {
	if p.Typed == nil {
		return nil
	}
	p.Typed.recOnce.Do(func() {
		rec := newValueRecord()
		valueTypesChecked(p, nil, nil, rec)
		p.Typed.rec = rec
	})
	return p.Typed.rec
}

// RuleType is RuleType.
func (p *Program) RuleType(name string) (string, bool) {
	if p.Typed == nil {
		return "", false
	}
	t, ok := p.Typed.shown[name]
	if !ok {
		return "", false
	}
	return t.String(), true
}

// JoinMembers returns t with the members of a join written as the node kinds that the typed
// generators give a Go type and a converter of their own: node[string] as a string node, node[_]
// as any node and error as an error node.
func JoinMembers(t Ty) Ty {
	u, ok := t.(JoinTy)
	if !ok {
		return t
	}
	alts := make([]Ty, len(u.Alts))
	for i, a := range u.Alts {
		n, node := a.(NodeTy)
		switch {
		case node && n.Name == "" && n.Value == TyString && len(n.Caps) == 0:
			alts[i] = TyMatch
		case anyNode(a):
			alts[i] = TyNode
		case a == TyErr:
			alts[i] = TyError
		default:
			alts[i] = a
		}
	}
	return JoinTy{alts}
}

type typedField struct {
	Name string
	T    Ty
}

// concreteType resolves aliases and normalized singleton joins to their node type.
// It does not report errors; validation still uses the type expression as written.
func (prog *Program) concreteType(t grammar.TypeExpr) (string, grammar.TypeDecl) {
	if ref, ok := t.(*grammar.TypeRef); ok {
		switch spec := prog.Types[ref.Name].(type) {
		case *grammar.TypeDef:
			return ref.Name, spec
		}
	}
	k := &TypeResolver{Prog: prog}
	if n, ok := k.Resolve(t).(NamedTy); ok && n.Kind == 's' {
		return n.Name, prog.Types[n.Name]
	}
	return "", nil
}

// StructType returns the struct type definition that the type named name denotes, following
// aliases, with the name of that definition; it returns "" and nil when name denotes no struct
// type.
func (prog *Program) StructType(name string) (string, *grammar.TypeDef) {
	switch spec := prog.Types[name].(type) {
	case *grammar.TypeDef:
		return name, spec
	case *grammar.TypeAlias:
		// Resolve only aliases; ordinary constructors need no temporary type expression.
	default:
		return "", nil
	}
	canonical, spec := prog.concreteType(&grammar.TypeRef{Name: name})
	st, _ := spec.(*grammar.TypeDef)
	return canonical, st
}

// concreteListElement reports whether the element type t of a list constructor, including the
// elements of nested list types, names no unknown type.
func concreteListElement(t Ty) bool {
	switch t := t.(type) {
	case ListTy:
		return concreteListElement(t.Elem)
	case OptTy:
		return concreteListElement(t.Elem)
	case JoinTy:
		for _, alt := range t.Alts {
			if !concreteListElement(alt) {
				return false
			}
		}
		return true
	}
	return t != TyAny
}

// termPos returns the position of term t for a diagnostic, or def if it has none.
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
	case *grammar.TupleLit:
		return t.Diagnostic()
	case *grammar.Call:
		return t.Diagnostic()
	case *grammar.Binary:
		return t.Diagnostic()
	case *grammar.Unary:
		return t.Diagnostic()
	case *grammar.Cast:
		return t.Diagnostic()
	case *grammar.Bind:
		return t.Diagnostic()
	case *grammar.Block:
		return t.Diagnostic()
	}
	return def
}
