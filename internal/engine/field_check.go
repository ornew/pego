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
	ftInt    = 'i'
	ftString = 's'
	ftBool   = 'b'
	ftNil    = 'z'
	ftNode   = 'n'
	ftNamed  = 'N' // a struct type or a named node type
	// ftStringNode is node[string], ftError error, the error value of recovery, and ftMissing
	// missing, the error value of a missing token. Grammars that refer to missing run only on the
	// Closure backend, so generated runtimes and the bytecode type text have no ftMissing.
	ftStringNode = 'S'
	ftError      = 'E'
	ftMissing    = 'M'
	ftList       = 'l'
	ftOptional   = 'o'
	ftUnion      = 'u'
	// ftNodeOf is node[T] for a T other than string and _: a node whose value is of type elem.
	ftNodeOf = 'v'
	// ftTuple is a tuple type, whose element types are alts.
	ftTuple = 't'
	// ftTop is the element type _ or any of a list type, which every value fits.
	ftTop = '_'
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
		case tyErr:
			f = &fieldType{kind: ftError}
		default:
			return nil
		}
	case nodeTy:
		// A declared type has no captures. node[_] and node[string] have their own kinds, which
		// take one step to check. A named node type is checked by its name.
		switch {
		case t.name != "":
			f = &fieldType{kind: ftNamed, name: t.name}
		case len(t.caps) > 0:
			return nil
		case t.value == tyTop || t.value == tyAny:
			f = &fieldType{kind: ftNode}
		case t.value == tyString:
			f = &fieldType{kind: ftStringNode}
		default:
			f = &fieldType{kind: ftNodeOf, elem: topFieldType(t.value)}
		}
	case tupleTy:
		f = &fieldType{kind: ftTuple, alts: []*fieldType{}}
		for _, e := range t.elems {
			f.alts = append(f.alts, topFieldType(e))
		}
	case namedTy:
		switch {
		case t == tyMatch:
			f = &fieldType{kind: ftStringNode}
		case t == tyError:
			f = &fieldType{kind: ftError}
		case t == tyMissing:
			f = &fieldType{kind: ftMissing}
		case t.kind == 'c':
			f = &fieldType{kind: ftNode}
		default:
			f = &fieldType{kind: ftNamed, name: t.name}
		}
	case listTy:
		// A list of _ or any only has to be a list, which takes one step to check whatever its
		// length.
		f = &fieldType{kind: ftList, elem: topFieldType(t.elem)}
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

// topFieldType is newFieldType for a part of a type, where _ and any are the type ftTop.
func topFieldType(t ty) *fieldType {
	if f := newFieldType(t); f != nil {
		return f
	}
	return &fieldType{kind: ftTop, text: t.String()}
}

// parseFieldType reads the text of a type as ty.String writes it, resolving names against the
// declared types (types, the set of their names); node[string], node[_] and error name
// the kinds of node. Bytecode carries field types this way.
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
		// A tuple (T, U), (T,) or (), or a parenthesized union.
		r.pos++
		tuple := &fieldType{kind: ftTuple, alts: []*fieldType{}}
		for !strings.HasPrefix(r.text[r.pos:], ")") {
			u, err := r.union()
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(r.text[r.pos:], ")") && len(tuple.alts) == 0 {
				if u.kind != ftUnion {
					return nil, fmt.Errorf("expected a parenthesized union or a tuple at %d", start)
				}
				r.pos++
				u.text = r.text[start:r.pos]
				return u, nil
			}
			tuple.alts = append(tuple.alts, u)
			switch {
			case strings.HasPrefix(r.text[r.pos:], ", "):
				r.pos += 2
			case strings.HasPrefix(r.text[r.pos:], ",)"):
				r.pos++
			case !strings.HasPrefix(r.text[r.pos:], ")"):
				return nil, fmt.Errorf("expected , or ) at %d", r.pos)
			}
		}
		r.pos++
		t = tuple
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
			switch {
			case strings.HasPrefix(r.text[r.pos:], "[string]"):
				r.pos += len("[string]")
				t = &fieldType{kind: ftStringNode}
			case strings.HasPrefix(r.text[r.pos:], "[_]"):
				r.pos += len("[_]")
				t = &fieldType{kind: ftNode}
			case strings.HasPrefix(r.text[r.pos:], "["):
				r.pos++
				e, err := r.union()
				if err != nil {
					return nil, err
				}
				if !strings.HasPrefix(r.text[r.pos:], "]") {
					return nil, fmt.Errorf("expected ] at %d", r.pos)
				}
				r.pos++
				t = &fieldType{kind: ftNodeOf, elem: e}
			default:
				return nil, fmt.Errorf("node without a type argument at %d", start)
			}
		case "error":
			t = &fieldType{kind: ftError}
		case "_", "any":
			t = &fieldType{kind: ftTop}
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

// moduleTypeNames returns the set of the names of a module's declared types.
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
	// without an action.
	all bool
	// fields holds the checked constructed fields. A nil type (in a loaded program) stands for
	// the declared type of the field.
	fields map[*grammar.FieldInit]ty
	// results holds the checked results, keyed by *grammar.RuleDef for a rule and by
	// *grammar.OperatorsRuleOperand or *grammar.OperatorsRuleOperator for a line of an
	// Operators Rule. A nil type (in a loaded program) stands for the rule's declared type.
	results map[any]ty
	// nodeMembers reports, for each member access of an action, whether it reads only node
	// members (.Start, .End, .Value) and never a struct field.
	nodeMembers map[*grammar.Member]bool
	// converts holds the node type constructors whose value is checked before it is converted,
	// with the underlying node type of the named node type. A nil type (in a loaded program)
	// stands for that type.
	converts map[*grammar.New]ty
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
	out := &checkInfo{fields: map[*grammar.FieldInit]ty{}, results: map[any]ty{}, converts: map[*grammar.New]ty{}}
	for i, rd := range from {
		fields, toFields := ruleFieldInits(rd), ruleFieldInits(to[i])
		results, toResults := ruleResults(rd), ruleResults(to[i])
		converts, toConverts := ruleConversions(rd), ruleConversions(to[i])
		if len(fields) != len(toFields) || len(results) != len(toResults) || len(converts) != len(toConverts) {
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
		for j, n := range converts {
			if t, ok := ci.converts[n]; ok {
				out.converts[toConverts[j]] = t
			}
		}
	}
	return out, nil
}

// empty reports whether nothing is checked.
func (ci *checkInfo) empty() bool {
	return ci == nil || !ci.all && len(ci.fields) == 0 && len(ci.results) == 0 && len(ci.converts) == 0
}

// convertCheckType returns the type the value of the node type constructor t must be checked
// against at run time before it is converted, the underlying node type of the named node type, or
// nil if it needs no check.
func (prog *Program) convertCheckType(t *grammar.New) ty {
	ci := prog.checks
	if ci.empty() {
		return nil
	}
	ct, ok := ci.converts[t]
	if !ci.all && !ok || ct != nil {
		return ct
	}
	if n, ok := prog.nodeTypeOf(t.Type); ok {
		return n.underlying()
	}
	return nil
}

// convertTypeCheck is convertCheckType for the closure backend, as a check type.
func (prog *Program) convertTypeCheck(t *grammar.New) *fieldType {
	if prog.checks.empty() {
		return nil
	}
	if v, ok := prog.fieldTypes.Load(t); ok {
		return v.(*fieldType)
	}
	var ft *fieldType
	if ct := prog.convertCheckType(t); ct != nil {
		ft = newFieldType(ct)
	}
	v, _ := prog.fieldTypes.LoadOrStore(t, ft)
	return v.(*fieldType)
}

// convertError reports a value that does not convert to the named node type typ.
func (p *parser) convertError(v any, typ string) error {
	return p.actionError("cannot convert ", typeName(v), " to ", typ)
}

// ruleConversions returns the node type constructors (constructors with a positional value) in
// the predicates and actions of rule rd, in a fixed order: the order compiled grammar files number
// them in.
func ruleConversions(rd *grammar.RuleDef) []*grammar.New {
	var news []*grammar.New
	walkRuleTerms(rd, func(x grammar.Term) {
		if n, ok := x.(*grammar.New); ok && len(n.Args) != 0 {
			news = append(news, n)
		}
	})
	return news
}

// resolveCheck resolves a declared type for a check, as a value type. It returns nil for any,
// which every value fits.
func (prog *Program) resolveCheck(t grammar.TypeExpr) ty {
	if t == nil {
		return nil
	}
	k := &typeResolver{prog: prog}
	if r := k.resolve(t); newFieldType(r) != nil {
		return r
	}
	return nil
}

// fieldCheckType returns the declared type a constructor must check the value of field fi of
// struct spec against at run time, or nil if it needs no check: the type checker proved every
// value of the field's expression assignable, or the type is any.
func (prog *Program) fieldCheckType(spec *grammar.TypeDef, fi *grammar.FieldInit) ty {
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
	if n, ok := prog.convertsResult(key, rd); ok {
		// A rule converting its value to a named node type, or an operator converting its node,
		// checks it against the underlying type.
		if t, ok := ci.results[key]; !ci.all && (!ok || t != nil) {
			return t
		}
		return n.underlying()
	}
	t, ok := ci.results[key]
	if !ci.all {
		if !ok || t != nil {
			return t
		}
		return prog.resolveCheck(declared)
	}
	switch key := key.(type) {
	case *grammar.RuleDef:
		if _, ok := key.Expr.(*grammar.OperatorsRule); ok {
			return nil // its lines are checked
		}
	case *grammar.OperatorsRuleOperator:
		if key.Action == nil {
			return nil // the tuple node of an operator
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

// fieldConforms reports whether v is a value of type t in the value model. It charges a step for
// each list or tuple element it examines. node[T] is a node whose value is of type T (node[_] any
// node with a value), and error an error value (an error or a missing token); a list or tuple type
// takes a list or tuple value, not the node of a repetition or sequence.
func (p *parser) fieldConforms(t *fieldType, v any) bool {
	n, isNode := v.(*Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	if isNode && n.shape() == shapeBox {
		return p.fieldConforms(t, n.box)
	}
	switch t.kind {
	case ftTop:
		return true
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
	shape := n.shape()
	valueNode := shape == shapeString || shape == shapeTuple || shape == shapeList
	switch t.kind {
	case ftNode:
		return valueNode
	case ftStringNode:
		return shape == shapeString
	case ftError:
		return shape == shapeError
	case ftMissing:
		return shape == shapeError && n.Type() == TypeMissing
	case ftNamed:
		return n.Type() == t.name
	case ftList:
		return shape == shapeListValue && p.elemsConform(t.elem, n.children)
	case ftTuple:
		return shape == shapeTupleValue && p.tupleConforms(t.alts, n.children)
	case ftNodeOf:
		return p.nodeValueConforms(t.elem, n, shape)
	}
	return false
}

// nodeValueConforms reports whether the value of node n, of shape shape, is of type t: a string
// node's value is a string, and a tuple or list node's value its elements.
func (p *parser) nodeValueConforms(t *fieldType, n *Node, shape nodeShape) bool {
	switch t.kind {
	case ftTop:
		return shape == shapeString || shape == shapeTuple || shape == shapeList
	case ftString:
		return shape == shapeString
	case ftList:
		return shape == shapeList && p.elemsConform(t.elem, n.children)
	case ftTuple:
		return shape == shapeTuple && p.tupleConforms(t.alts, n.children)
	case ftOptional:
		return p.nodeValueConforms(t.elem, n, shape) // a node always has a value
	case ftUnion:
		for _, a := range t.alts {
			if p.nodeValueConforms(a, n, shape) {
				return true
			}
		}
	}
	// No node has a value of another type yet.
	return false
}

// elemsConform reports whether every element of a list is of type t, charging a step for each.
func (p *parser) elemsConform(t *fieldType, elems []*Node) bool {
	if t.kind == ftTop {
		return true
	}
	for _, c := range elems {
		p.stepWork()
		if !p.fieldConforms(t, c) {
			return false
		}
	}
	return true
}

// tupleConforms reports whether the elements of a tuple are of the types ts, charging a step for
// each.
func (p *parser) tupleConforms(ts []*fieldType, elems []*Node) bool {
	if len(elems) != len(ts) {
		return false
	}
	for i, c := range elems {
		p.stepWork()
		if !p.fieldConforms(ts[i], c) {
			return false
		}
	}
	return true
}

// castType returns the type cast t checks its value against, or nil if every value fits it.
func (prog *Program) castType(t *grammar.Cast) *fieldType {
	if v, ok := prog.fieldTypes.Load(t); ok {
		return v.(*fieldType)
	}
	var ft *fieldType
	if r := prog.resolveCheck(t.Type); r != nil {
		ft = newFieldType(r)
	}
	v, _ := prog.fieldTypes.LoadOrStore(t, ft)
	return v.(*fieldType)
}

// castError reports a cast value that is not of the type it is cast to.
func (p *parser) castError(v any, t *fieldType) error {
	return p.actionError("cannot cast ", typeName(v), " to ", t.text)
}

// fieldTypeError reports a constructed field value that does not fit its declared type.
func (p *parser) fieldTypeError(v any, t *fieldType, field, typ string) error {
	return p.actionError("cannot use ", typeName(v), " as ", t.text, " in field ", field, " of ", typ)
}

// checkResult fails the parse if v, a result of rule rule, does not fit t, the rule's declared
// type.
func (p *parser) checkResult(t *fieldType, v *Node, rule string) {
	if u := unboxNode(v); !p.fieldConforms(t, u) {
		p.fail("cannot use %s as %s in result of rule %s", typeName(u), t.text, rule)
	}
}
