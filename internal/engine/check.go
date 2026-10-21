package engine

import (
	"sync"

	"github.com/ornew/pego/grammar"
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

// checkTypes checks the types of the whole grammar.
func checkTypes(prog *Program, errs *ErrorList) {
	checkTypesControlled(prog, errs, nil)
}

func checkTypesControlled(prog *Program, errs *ErrorList, control *compileControl) {
	defer dropRepeatedErrors(errs, len(*errs))
	k := &typeResolver{control: control, prog: prog, errs: errs, report: true}
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
	values, valueErrs := valueTypesChecked(prog, control, nodeMembers)
	*errs = append(*errs, valueErrs...)

	// Keep the types for code generation (typedGo).
	info := &typeInfo{fields: map[string][]typedField{}, aliases: map[string]ty{}, nodeMembers: nodeMembers, values: values, valueErrs: valueErrs}
	info.valuesOnce.Do(func() {})
	info.shown = shownRuleTypes(prog, rules, values, control)
	k.report = false
	for _, td := range control.types(prog.Grammar) {
		control.poll()
		switch spec := td.(type) {
		case *grammar.TypeDef:
			for _, f := range defFields(spec) {
				control.poll()
				info.fields[td.DeclName()] = append(info.fields[td.DeclName()], typedField{f.Name, k.resolve(f.Type)})
			}
		case *grammar.TypeAlias:
			info.aliases[td.DeclName()] = unionMembers(k.resolveNamed(td.DeclName(), td.Diagnostic()))
		}
	}
	prog.typed = info
}

// typeInfo holds the types found by the type checker, for code generation.
type typeInfo struct {
	// values holds the type of each rule's value in the value model (Program.valueTypes).
	values     map[string]ty
	valueErrs  ErrorList     // the errors of actions in the value model
	shown      map[string]ty // the type of each rule as RuleType shows it (shownRuleTypes)
	valuesOnce sync.Once
	fields     map[string][]typedField // the fields of each struct type, in declaration order
	aliases    map[string]ty           // the type each alias stands for
	// nodeMembers reports, for each member access of an action, whether it reads only node
	// members (.Start, .End, .Value) and never a struct field.
	nodeMembers map[*grammar.Member]bool
}

// RuleType returns the type of the value of the rule name: its declared type, or the inferred
// type of its value (for example "[]Expr | node[[]node[string]]"). In an inferred type, a
// reference to another rule is that rule's declared type, or the rule's name if it has none. It
// reports false if the program has no such rule or its types were not checked
// (a program loaded from a compiled grammar).
func (p *Program) RuleType(name string) (string, bool) {
	if p.typed == nil {
		return "", false
	}
	t, ok := p.typed.shown[name]
	if !ok {
		return "", false
	}
	return t.String(), true
}

// unionMembers returns t with the members of a union written as the node kinds that the typed
// generators give a Go type and a converter of their own: node[string] as a string node, node[_]
// as any node and error as an error node.
func unionMembers(t ty) ty {
	u, ok := t.(unionTy)
	if !ok {
		return t
	}
	alts := make([]ty, len(u.alts))
	for i, a := range u.alts {
		n, node := a.(nodeTy)
		switch {
		case node && n.name == "" && n.value == tyString && len(n.caps) == 0:
			alts[i] = tyMatch
		case anyNode(a):
			alts[i] = tyNode
		case a == tyErr:
			alts[i] = tyError
		default:
			alts[i] = a
		}
	}
	return unionTy{alts}
}

type typedField struct {
	name string
	t    ty
}

// concreteType resolves aliases and normalized singleton unions to their node type.
// It does not report errors; validation still uses the type expression as written.
func (prog *Program) concreteType(t grammar.TypeExpr) (string, grammar.TypeDecl) {
	if ref, ok := t.(*grammar.TypeRef); ok {
		switch spec := prog.types[ref.Name].(type) {
		case *grammar.TypeDef:
			return ref.Name, spec
		}
	}
	k := &typeResolver{prog: prog}
	if n, ok := k.resolve(t).(namedTy); ok && n.kind == 's' {
		return n.name, prog.types[n.name]
	}
	return "", nil
}

func (prog *Program) structType(name string) (string, *grammar.TypeDef) {
	switch spec := prog.types[name].(type) {
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
func concreteListElement(t ty) bool {
	switch t := t.(type) {
	case listTy:
		return concreteListElement(t.elem)
	case optTy:
		return concreteListElement(t.elem)
	case unionTy:
		for _, alt := range t.alts {
			if !concreteListElement(alt) {
				return false
			}
		}
		return true
	}
	return t != tyAny
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
	case *grammar.Assign:
		return t.Diagnostic()
	case *grammar.Block:
		return t.Diagnostic()
	}
	return def
}
