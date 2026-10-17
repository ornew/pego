package engine

import (
	"fmt"
	"strings"

	"github.com/ornew/pego/grammar"
)

// fieldType is a declared struct field type that a constructor checks a dynamically typed value
// against at run time. Its text is the type as messages and bytecode write it.
type fieldType struct {
	kind byte // see the ft constants
	name string
	elem *fieldType
	alts []*fieldType
	text string
}

const (
	ftInt      = 'i'
	ftString   = 's'
	ftBool     = 'b'
	ftNil      = 'z'
	ftNode     = 'n'
	ftTerminal = 't'
	ftNamed    = 'N'
	ftList     = 'l'
	ftOptional = 'o'
	ftUnion    = 'u'
)

// newFieldType converts a resolved declared type. It returns nil for any, which every value fits.
func newFieldType(t ty) *fieldType {
	var f *fieldType
	switch t := t.(type) {
	case basicTy:
		switch t {
		case tyInt:
			f = &fieldType{kind: ftInt}
		case tyString:
			f = &fieldType{kind: ftString}
		case tyBool:
			f = &fieldType{kind: ftBool}
		case tyNil:
			f = &fieldType{kind: ftNil}
		case tyNode:
			f = &fieldType{kind: ftNode}
		case tyTerminal:
			f = &fieldType{kind: ftTerminal}
		default:
			return nil
		}
	case namedTy:
		f = &fieldType{kind: ftNamed, name: t.name}
	case listTy:
		e := newFieldType(t.elem)
		if e == nil {
			return nil // a declared []any cannot exist: list elements are nodes
		}
		f = &fieldType{kind: ftList, elem: e}
	case optTy:
		e := newFieldType(t.elem)
		if e == nil {
			return nil
		}
		f = &fieldType{kind: ftOptional, elem: e}
	case unionTy:
		f = &fieldType{kind: ftUnion}
		for _, a := range t.alts {
			e := newFieldType(a)
			if e == nil {
				return nil
			}
			f.alts = append(f.alts, e)
		}
	default:
		return nil
	}
	f.text = t.String()
	return f
}

// parseFieldType reads the text of a type as ty.String writes it, resolving names against the
// declared terminal and struct types (types) and the reserved node types. Bytecode carries
// field types this way.
func parseFieldType(text string, types map[string]bool) (*fieldType, error) {
	r := fieldTypeReader{text: text, types: types}
	t, err := r.union()
	if err == nil && r.pos != len(text) {
		err = fmt.Errorf("unexpected %q", text[r.pos:])
	}
	if err != nil {
		return nil, fmt.Errorf("invalid field type %q: %w", text, err)
	}
	return t, nil
}

type fieldTypeReader struct {
	text  string
	pos   int
	types map[string]bool
	depth int
}

func (r *fieldTypeReader) union() (*fieldType, error) {
	start := r.pos
	first, err := r.term()
	if err != nil || !strings.HasPrefix(r.text[r.pos:], " | ") {
		return first, err
	}
	u := &fieldType{kind: ftUnion, alts: []*fieldType{first}}
	for strings.HasPrefix(r.text[r.pos:], " | ") {
		r.pos += 3
		t, err := r.term()
		if err != nil {
			return nil, err
		}
		u.alts = append(u.alts, t)
	}
	u.text = r.text[start:r.pos]
	return u, nil
}

func (r *fieldTypeReader) term() (*fieldType, error) {
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > maxDepth {
		return nil, fmt.Errorf("nesting exceeds %d", maxDepth)
	}
	start := r.pos
	rest := r.text[r.pos:]
	var t *fieldType
	switch {
	case strings.HasPrefix(rest, "[]"), strings.HasPrefix(rest, "*"):
		kind := byte(ftList)
		if rest[0] == '*' {
			kind, r.pos = ftOptional, r.pos+1
		} else {
			r.pos += 2
		}
		e, err := r.term()
		if err != nil {
			return nil, err
		}
		t = &fieldType{kind: kind, elem: e}
	case strings.HasPrefix(rest, "("):
		r.pos++
		u, err := r.union()
		if err != nil {
			return nil, err
		}
		if u.kind != ftUnion || !strings.HasPrefix(r.text[r.pos:], ")") {
			return nil, fmt.Errorf("expected a parenthesized union at %d", start)
		}
		r.pos++
		u.text = r.text[start:r.pos]
		return u, nil
	default:
		end := r.pos
		for end < len(r.text) && (r.text[end] == '_' || r.text[end] >= '0' && r.text[end] <= '9' || r.text[end] >= 'a' && r.text[end] <= 'z' || r.text[end] >= 'A' && r.text[end] <= 'Z') {
			end++
		}
		name := r.text[r.pos:end]
		r.pos = end
		switch name {
		case "int":
			t = &fieldType{kind: ftInt}
		case "string":
			t = &fieldType{kind: ftString}
		case "bool":
			t = &fieldType{kind: ftBool}
		case "nil":
			t = &fieldType{kind: ftNil}
		case "node":
			t = &fieldType{kind: ftNode}
		case "terminal":
			t = &fieldType{kind: ftTerminal}
		case TypeMatch, TypeSeq, TypeList, TypeOperator, TypeError, TypeMissing:
			t = &fieldType{kind: ftNamed, name: name}
		default:
			if !r.types[name] {
				return nil, fmt.Errorf("unknown type %q at %d", name, start)
			}
			t = &fieldType{kind: ftNamed, name: name}
		}
	}
	t.text = r.text[start:r.pos]
	return t, nil
}

// moduleTypeNames returns the names of a module's declared types.
func moduleTypeNames(m *Module) map[string]bool {
	names := make(map[string]bool, len(m.Types))
	for _, t := range m.Types {
		if t.Name >= 0 && t.Name < len(m.Strings) {
			names[m.Strings[t.Name]] = true
		}
	}
	return names
}

// checkInfo holds the values a program checks at run time against their declared types:
// constructed struct field values, and the results of rules and Operators Rule lines with a
// declared type.
type checkInfo struct {
	// all reports a program compiled without type checking. It checks every constructed field and
	// every result whose declared type is not any, except the values of Operators Rule operators
	// without an action and terminal conversions.
	all bool
	// fields holds the checked constructed fields. A nil type (in a loaded program) stands for
	// the declared type of the field.
	fields map[*grammar.FieldInit]ty
	// results holds the checked results, keyed by *grammar.RuleDef for a rule and by
	// *grammar.OperatorsRuleOperand or *grammar.OperatorsRuleOperator for a line of an
	// Operators Rule. A nil type (in a loaded program) stands for the rule's declared type.
	results map[any]ty
}

// rekey returns the checks keyed by the nodes of the rules to instead of those of the rules from:
// the same grammar before and after attribute normalization, which copies nodes but keeps their
// order.
func (ci *checkInfo) rekey(from, to []*grammar.RuleDef) (*checkInfo, error) {
	if ci.all {
		return ci, nil
	}
	if len(from) != len(to) {
		return nil, fmt.Errorf("normalization changed the number of rules from %d to %d", len(from), len(to))
	}
	out := &checkInfo{fields: map[*grammar.FieldInit]ty{}, results: map[any]ty{}}
	for i, rd := range from {
		fields, toFields := ruleFieldInits(rd), ruleFieldInits(to[i])
		results, toResults := ruleResults(rd), ruleResults(to[i])
		if len(fields) != len(toFields) || len(results) != len(toResults) {
			return nil, fmt.Errorf("normalization changed the values of rule %s", rd.Name)
		}
		for j, f := range fields {
			if t, ok := ci.fields[f.FieldInit]; ok {
				out.fields[toFields[j].FieldInit] = t
			}
		}
		for j, key := range results {
			if t, ok := ci.results[key]; ok {
				out.results[toResults[j]] = t
			}
		}
	}
	return out, nil
}

// empty reports whether nothing is checked.
func (ci *checkInfo) empty() bool {
	return ci == nil || !ci.all && len(ci.fields) == 0 && len(ci.results) == 0
}

// resolveCheck resolves a declared type for a check. It returns nil for any, which every value fits.
func (prog *Program) resolveCheck(t grammar.TypeExpr) ty {
	if t == nil {
		return nil
	}
	k := &checker{prog: prog, resolving: map[string]bool{}}
	if r := k.resolve(t); newFieldType(r) != nil {
		return r
	}
	return nil
}

// fieldCheckType returns the declared type a constructor must check the value of field fi of
// struct spec against at run time, or nil if it needs no check: the type checker proved every
// value of the field's expression assignable, or the type is any.
func (prog *Program) fieldCheckType(spec *grammar.StructSpec, fi *grammar.FieldInit) ty {
	ci := prog.checks
	if ci.empty() {
		return nil
	}
	t, ok := ci.fields[fi]
	if !ci.all && !ok || t != nil {
		return t
	}
	if spec == nil {
		return nil
	}
	if f := fieldOf(spec, fi.Name); f != nil {
		return prog.resolveCheck(f.Type)
	}
	return nil
}

// resultCheckType returns the declared type the result of a rule or Operators Rule line must be
// checked against at run time, or nil if it needs no check. key identifies the rule or line (see
// checkInfo.results), and rd is the rule.
func (prog *Program) resultCheckType(key any, rd *grammar.RuleDef) ty {
	declared := rd.Type
	ci := prog.checks
	if ci.empty() {
		return nil
	}
	t, ok := ci.results[key]
	if !ci.all {
		if !ok || t != nil {
			return t
		}
		return prog.resolveCheck(declared)
	}
	if rd.Action == nil {
		if _, spec := prog.concreteType(declared); spec != nil {
			if _, ok := spec.(*grammar.TerminalSpec); ok {
				return nil // a conversion, of the rule or of an Operators Rule's lines
			}
		}
	}
	switch key := key.(type) {
	case *grammar.RuleDef:
		if _, ok := key.Expr.(*grammar.OperatorsRule); ok {
			return nil // its lines are checked
		}
	case *grammar.OperatorsRuleOperator:
		if key.Action == nil {
			return nil // an Operator value
		}
	}
	return prog.resolveCheck(declared)
}

// assignResultChecks gives the rules and Operators Rule lines that produce values their run-time
// result checks.
func (prog *Program) assignResultChecks() {
	for _, r := range prog.rules {
		if r.novalue {
			continue
		}
		if t := prog.resultCheckType(r.def, r.def); t != nil {
			r.resultCheck = newFieldType(t)
		}
		if pr := r.operatorsRule; pr != nil {
			var lines []*operatorsRuleLine
			lines = append(lines, pr.operands...)
			for _, op := range pr.prefix {
				lines = append(lines, op.line)
			}
			for _, op := range pr.led {
				lines = append(lines, op.line)
			}
			for _, l := range lines {
				if t := prog.resultCheckType(l.key, r.def); t != nil {
					l.check = newFieldType(t)
				}
			}
		}
	}
}

// constructorChecks returns the closure backend's checks of the fields of constructor t, by field
// index, or nil if no field needs one.
func (prog *Program) constructorChecks(t *grammar.New) []*fieldType {
	if ci := prog.checks; ci.empty() || !ci.all && len(ci.fields) == 0 {
		return nil
	}
	if v, ok := prog.fieldTypes.Load(t); ok {
		return v.([]*fieldType)
	}
	_, spec := prog.structType(t.Type)
	var checks []*fieldType
	for i, fi := range t.Fields {
		if ft := prog.fieldCheckType(spec, fi); ft != nil {
			if checks == nil {
				checks = make([]*fieldType, len(t.Fields))
			}
			checks[i] = newFieldType(ft)
		}
	}
	v, _ := prog.fieldTypes.LoadOrStore(t, checks)
	return v.([]*fieldType)
}

// constructedField is a field of a struct constructor.
type constructedField struct {
	*grammar.FieldInit
	new *grammar.New
}

// ruleFieldInits returns the constructed fields in the predicates and actions of rule rd, in a fixed order: the
// order compiled grammar files number them in.
func ruleFieldInits(rd *grammar.RuleDef) []constructedField {
	var fields []constructedField
	walkRuleTerms(rd, func(x grammar.Term) {
		if n, ok := x.(*grammar.New); ok {
			for _, fi := range n.Fields {
				fields = append(fields, constructedField{fi, n})
			}
		}
	})
	return fields
}

// ruleResults returns the keys of the results of rule rd (see checkInfo.results), in the order
// compiled grammar files number them in.
func ruleResults(rd *grammar.RuleDef) []any {
	pr, ok := rd.Expr.(*grammar.OperatorsRule)
	if !ok {
		return []any{rd}
	}
	var keys []any
	for _, o := range pr.Operands {
		keys = append(keys, o)
	}
	for _, l := range pr.Levels {
		for _, op := range l.Operators {
			keys = append(keys, op)
		}
	}
	return keys
}

// fieldConforms reports whether v is a value of type t. It charges a step for each list element
// it examines. Error and Missing nodes stand for any node type, and for an empty list.
func (p *parser) fieldConforms(t *fieldType, v any) bool {
	n, isNode := v.(*Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	switch t.kind {
	case ftInt:
		_, ok := v.(int)
		return ok
	case ftString:
		_, ok := v.(string)
		return ok
	case ftBool:
		_, ok := v.(bool)
		return ok
	case ftNil:
		return v == nil
	case ftOptional:
		return v == nil || p.fieldConforms(t.elem, v)
	case ftUnion:
		for _, a := range t.alts {
			if p.fieldConforms(a, v) {
				return true
			}
		}
		return false
	}
	if !isNode {
		return false
	}
	typ := n.Type()
	if typ == TypeError || typ == TypeMissing {
		return true
	}
	switch t.kind {
	case ftNode:
		return true
	case ftTerminal:
		return n.IsTerminal()
	case ftNamed:
		return typ == t.name
	case ftList:
		if typ != TypeList {
			return false
		}
		for _, c := range n.Children {
			p.stepWork()
			if !p.fieldConforms(t.elem, c) {
				return false
			}
		}
		return true
	}
	return false
}

// fieldTypeError reports a constructed field value that does not fit its declared type.
func (p *parser) fieldTypeError(v any, t *fieldType, field, typ string) error {
	return p.actionError("cannot use ", typeName(v), " as ", t.text, " in field ", field, " of ", typ)
}

// checkResult fails the parse if v, a result of rule rule, does not fit t, the rule's declared
// type.
func (p *parser) checkResult(t *fieldType, v *Node, rule string) {
	if !p.fieldConforms(t, v) {
		p.fail("cannot use %s as %s in result of rule %s", typeName(nodeOrNil(v)), t.text, rule)
	}
}
