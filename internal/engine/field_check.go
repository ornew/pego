package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/bytecode"
	"github.com/ornew/pego/internal/value"
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
	ftJoin       = 'u'
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
	case joinTy:
		f = &fieldType{kind: ftJoin}
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
	t, err := r.join()
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

func (r *fieldTypeReader) join() (*fieldType, error) {
	start := r.pos
	first, err := r.term()
	if err != nil || !strings.HasPrefix(r.text[r.pos:], " | ") {
		return first, err
	}
	u := &fieldType{kind: ftJoin, alts: []*fieldType{first}}
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

// maxDepth bounds the nesting of a field type that fieldTypeReader reads.
const maxDepth = 10000

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
		// A tuple (T, U), (T,) or (), or a parenthesized join.
		r.pos++
		tuple := &fieldType{kind: ftTuple, alts: []*fieldType{}}
		for !strings.HasPrefix(r.text[r.pos:], ")") {
			u, err := r.join()
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(r.text[r.pos:], ")") && len(tuple.alts) == 0 {
				if u.kind != ftJoin {
					return nil, fmt.Errorf("expected a parenthesized join or a tuple at %d", start)
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
				e, err := r.join()
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
		case "_":
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

// validateModule checks the module m (bytecode.ValidateModule), with the field types parsed against
// the types that m declares.
func validateModule(m *bytecode.Module) error {
	var typeNames map[string]bool
	return bytecode.ValidateModule(m, func(text string) error {
		if typeNames == nil {
			typeNames = moduleTypeNames(m)
		}
		_, err := parseFieldType(text, typeNames)
		return err
	})
}

// moduleTypeNames returns the set of the names of a module's declared types.
func moduleTypeNames(m *bytecode.Module) map[string]bool {
	names := make(map[string]bool, len(m.Types))
	for _, t := range m.Types {
		if t.Name >= 0 && t.Name < len(m.Strings) {
			names[m.Strings[t.Name]] = true
		}
	}
	return names
}

// convertError reports a value that does not convert to the named node type typ.
func (p *parser) convertError(v any, typ string) error {
	return p.actionError("cannot convert ", typeName(v), " to ", typ)
}

// fieldConforms reports whether v is a value of type t in the value model. It charges a step for
// each list or tuple element it examines. node[T] is a node whose value is of type T (node[_] any
// node with a value), and error an error value (an error or a missing token); a list or tuple type
// takes a list or tuple value, not the node of a repetition or sequence.
func (p *parser) fieldConforms(t *fieldType, v any) bool {
	n, isNode := v.(*value.Node)
	if isNode && n == nil {
		v, isNode = nil, false
	}
	if isNode && value.ShapeOf(n) == value.ShapeBox {
		return p.fieldConforms(t, value.D(n).Box)
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
	case ftJoin:
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
	shape := value.ShapeOf(n)
	valueNode := shape == value.ShapeString || shape == value.ShapeTuple || shape == value.ShapeList
	switch t.kind {
	case ftNode:
		return valueNode
	case ftStringNode:
		return shape == value.ShapeString
	case ftError:
		return shape == value.ShapeError
	case ftMissing:
		return shape == value.ShapeError && n.Type() == value.TypeMissing
	case ftNamed:
		return n.Type() == t.name
	case ftList:
		return shape == value.ShapeListValue && p.elemsConform(t.elem, value.D(n).Children)
	case ftTuple:
		return shape == value.ShapeTupleValue && p.tupleConforms(t.alts, value.D(n).Children)
	case ftNodeOf:
		return p.nodeValueConforms(t.elem, n, shape)
	}
	return false
}

// nodeValueConforms reports whether the value of node n, of shape shape, is of type t: a string
// node's value is a string, and a tuple or list node's value its elements.
func (p *parser) nodeValueConforms(t *fieldType, n *value.Node, shape value.Shape) bool {
	switch t.kind {
	case ftTop:
		return shape == value.ShapeString || shape == value.ShapeTuple || shape == value.ShapeList
	case ftString:
		return shape == value.ShapeString
	case ftList:
		return shape == value.ShapeList && p.elemsConform(t.elem, value.D(n).Children)
	case ftTuple:
		return shape == value.ShapeTuple && p.tupleConforms(t.alts, value.D(n).Children)
	case ftOptional:
		return p.nodeValueConforms(t.elem, n, shape) // a node always has a value
	case ftJoin:
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
func (p *parser) elemsConform(t *fieldType, elems []*value.Node) bool {
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
func (p *parser) tupleConforms(ts []*fieldType, elems []*value.Node) bool {
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

// resolveCheck resolves a type for a cast, as a value type. It returns nil for _, which every
// value fits.
func (prog *Program) resolveCheck(t grammar.TypeExpr) ty {
	if t == nil {
		return nil
	}
	if r := (&typeResolver{prog: prog}).resolve(t); newFieldType(r) != nil {
		return r
	}
	return nil
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

// castError reports a cast value that is not of the type it is cast to. pos is the input position
// where the evaluation started, and gline:gcol the grammar position of the cast (0 if unknown).
func (p *parser) castError(v any, t *fieldType, pos, gline, gcol int) error {
	line, col := p.input.lineCol(pos)
	parts := []string{"cannot cast ", typeName(v), " to ", t.text, " at input position ", strconv.Itoa(pos),
		" (line ", strconv.Itoa(line), ", column ", strconv.Itoa(col), ")"}
	if gline > 0 {
		parts = append(parts, "; cast at grammar position ", strconv.Itoa(gline), ":", strconv.Itoa(gcol))
	}
	return p.actionError(parts...)
}

// castPos returns the grammar position of the cast t as line and column, or 0, 0 when the program
// records no source information or the position is unknown.
func (prog *Program) castPos(t *grammar.Cast) (int, int) {
	if !prog.sourceInfo {
		return 0, 0
	}
	if pos := t.Diagnostic(); pos.IsValid() {
		return pos.Line, pos.Col
	}
	return 0, 0
}
