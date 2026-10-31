package compile

import (
	"fmt"
	"strings"

	"github.com/ornew/zeroparse/grammar"
	"github.com/ornew/zeroparse/internal/bytecode"
)

// FieldType is a declared struct field type that a constructor checks a dynamically typed value
// against at run time. Its text is the type as messages and bytecode write it.
type FieldType struct {
	Kind byte // see the Ft constants
	Name string
	Elem *FieldType
	Alts []*FieldType
	Text string
}

// The kinds of a FieldType.
const (
	FtInt    = 'i'
	FtString = 's'
	FtBool   = 'b'
	FtNil    = 'z'
	FtNode   = 'n'
	FtNamed  = 'N' // a struct type or a named node type
	// FtStringNode is node[string], FtError error, the error value of recovery, and FtMissing
	// missing, the error value of a missing token. Grammars that refer to missing run only on the
	// Closure backend, so generated runtimes and the bytecode type text have no FtMissing.
	FtStringNode = 'S'
	FtError      = 'E'
	FtMissing    = 'M'
	FtList       = 'l'
	FtOptional   = 'o'
	FtJoin       = 'u'
	// FtNodeOf is node[T] for a T other than string and _: a node whose value is of type elem.
	FtNodeOf = 'v'
	// FtTuple is a tuple type, whose element types are alts.
	FtTuple = 't'
	// FtTop is the element type _ or any of a list type, which every value fits.
	FtTop = '_'
)

// NewFieldType converts a resolved declared type. It returns nil for any, which every value fits.
func NewFieldType(t Ty) *FieldType {
	var f *FieldType
	switch t := t.(type) {
	case BasicTy:
		switch t {
		case TyInt:
			f = &FieldType{Kind: FtInt}
		case TyString:
			f = &FieldType{Kind: FtString}
		case TyBool:
			f = &FieldType{Kind: FtBool}
		case TyNil:
			f = &FieldType{Kind: FtNil}
		case TyNode:
			f = &FieldType{Kind: FtNode}
		case TyErr:
			f = &FieldType{Kind: FtError}
		default:
			return nil
		}
	case NodeTy:
		// A declared type has no captures. node[_] and node[string] have their own kinds, which
		// take one step to check. A named node type is checked by its name.
		switch {
		case t.Name != "":
			f = &FieldType{Kind: FtNamed, Name: t.Name}
		case len(t.Caps) > 0:
			return nil
		case t.Value == TyTop || t.Value == TyAny:
			f = &FieldType{Kind: FtNode}
		case t.Value == TyString:
			f = &FieldType{Kind: FtStringNode}
		default:
			f = &FieldType{Kind: FtNodeOf, Elem: topFieldType(t.Value)}
		}
	case TupleTy:
		f = &FieldType{Kind: FtTuple, Alts: []*FieldType{}}
		for _, e := range t.Elems {
			f.Alts = append(f.Alts, topFieldType(e))
		}
	case NamedTy:
		switch {
		case t == TyMatch:
			f = &FieldType{Kind: FtStringNode}
		case t == TyError:
			f = &FieldType{Kind: FtError}
		case t == TyMissing:
			f = &FieldType{Kind: FtMissing}
		case t.Kind == 'c':
			f = &FieldType{Kind: FtNode}
		default:
			f = &FieldType{Kind: FtNamed, Name: t.Name}
		}
	case ListTy:
		// A list of _ or any only has to be a list, which takes one step to check whatever its
		// length.
		f = &FieldType{Kind: FtList, Elem: topFieldType(t.Elem)}
	case OptTy:
		e := NewFieldType(t.Elem)
		if e == nil {
			return nil
		}
		f = &FieldType{Kind: FtOptional, Elem: e}
	case JoinTy:
		f = &FieldType{Kind: FtJoin}
		for _, a := range t.Alts {
			e := NewFieldType(a)
			if e == nil {
				return nil
			}
			f.Alts = append(f.Alts, e)
		}
	default:
		return nil
	}
	f.Text = t.String()
	return f
}

// topFieldType is newFieldType for a part of a type, where _ and any are the type ftTop.
func topFieldType(t Ty) *FieldType {
	if f := NewFieldType(t); f != nil {
		return f
	}
	return &FieldType{Kind: FtTop, Text: t.String()}
}

// ParseFieldType reads the text of a type as ty.String writes it, resolving names against the
// declared types (types, the set of their names); node[string], node[_] and error name
// the kinds of node. Bytecode carries field types this way.
func ParseFieldType(text string, types map[string]bool) (*FieldType, error) {
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

func (r *fieldTypeReader) join() (*FieldType, error) {
	start := r.pos
	first, err := r.term()
	if err != nil || !strings.HasPrefix(r.text[r.pos:], " | ") {
		return first, err
	}
	u := &FieldType{Kind: FtJoin, Alts: []*FieldType{first}}
	for strings.HasPrefix(r.text[r.pos:], " | ") {
		r.pos += 3
		t, err := r.term()
		if err != nil {
			return nil, err
		}
		u.Alts = append(u.Alts, t)
	}
	u.Text = r.text[start:r.pos]
	return u, nil
}

// maxDepth bounds the nesting of a field type that fieldTypeReader reads.
const maxDepth = 10000

func (r *fieldTypeReader) term() (*FieldType, error) {
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > maxDepth {
		return nil, fmt.Errorf("nesting exceeds %d", maxDepth)
	}
	start := r.pos
	rest := r.text[r.pos:]
	var t *FieldType
	switch {
	case strings.HasPrefix(rest, "[]"), strings.HasPrefix(rest, "*"):
		kind := byte(FtList)
		if rest[0] == '*' {
			kind, r.pos = FtOptional, r.pos+1
		} else {
			r.pos += 2
		}
		e, err := r.term()
		if err != nil {
			return nil, err
		}
		t = &FieldType{Kind: kind, Elem: e}
	case strings.HasPrefix(rest, "("):
		// A tuple (T, U), (T,) or (), or a parenthesized join.
		r.pos++
		tuple := &FieldType{Kind: FtTuple, Alts: []*FieldType{}}
		for !strings.HasPrefix(r.text[r.pos:], ")") {
			u, err := r.join()
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(r.text[r.pos:], ")") && len(tuple.Alts) == 0 {
				if u.Kind != FtJoin {
					return nil, fmt.Errorf("expected a parenthesized join or a tuple at %d", start)
				}
				r.pos++
				u.Text = r.text[start:r.pos]
				return u, nil
			}
			tuple.Alts = append(tuple.Alts, u)
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
			t = &FieldType{Kind: FtInt}
		case "string":
			t = &FieldType{Kind: FtString}
		case "bool":
			t = &FieldType{Kind: FtBool}
		case "nil":
			t = &FieldType{Kind: FtNil}
		case "node":
			switch {
			case strings.HasPrefix(r.text[r.pos:], "[string]"):
				r.pos += len("[string]")
				t = &FieldType{Kind: FtStringNode}
			case strings.HasPrefix(r.text[r.pos:], "[_]"):
				r.pos += len("[_]")
				t = &FieldType{Kind: FtNode}
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
				t = &FieldType{Kind: FtNodeOf, Elem: e}
			default:
				return nil, fmt.Errorf("node without a type argument at %d", start)
			}
		case "error":
			t = &FieldType{Kind: FtError}
		case "_":
			t = &FieldType{Kind: FtTop}
		default:
			if !r.types[name] {
				return nil, fmt.Errorf("unknown type %q at %d", name, start)
			}
			t = &FieldType{Kind: FtNamed, Name: name}
		}
	}
	t.Text = r.text[start:r.pos]
	return t, nil
}

// ValidateModule checks the module m (bytecode.ValidateModule), with the field types parsed against
// the types that m declares.
func ValidateModule(m *bytecode.Module) error {
	var typeNames map[string]bool
	return bytecode.ValidateModule(m, func(text string) error {
		if typeNames == nil {
			typeNames = ModuleTypeNames(m)
		}
		_, err := ParseFieldType(text, typeNames)
		return err
	})
}

// ModuleTypeNames returns the set of the names of a module's declared types.
func ModuleTypeNames(m *bytecode.Module) map[string]bool {
	names := make(map[string]bool, len(m.Types))
	for _, t := range m.Types {
		if t.Name >= 0 && t.Name < len(m.Strings) {
			names[m.Strings[t.Name]] = true
		}
	}
	return names
}

// ResolveCheck resolves a type for a cast, as a value type. It returns nil for _, which every
// value fits.
func (prog *Program) ResolveCheck(t grammar.TypeExpr) Ty {
	if t == nil {
		return nil
	}
	if r := (&TypeResolver{Prog: prog}).Resolve(t); NewFieldType(r) != nil {
		return r
	}
	return nil
}

// CastType returns the type cast t checks its value against, or nil if every value fits it.
func (prog *Program) CastType(t *grammar.Cast) *FieldType {
	if v, ok := prog.fieldTypes.Load(t); ok {
		return v.(*FieldType)
	}
	var ft *FieldType
	if r := prog.ResolveCheck(t.Type); r != nil {
		ft = NewFieldType(r)
	}
	v, _ := prog.fieldTypes.LoadOrStore(t, ft)
	return v.(*FieldType)
}

// CastPos returns the grammar position of the cast t as line and column, or 0, 0 when the program
// records no source information or the position is unknown.
func (prog *Program) CastPos(t *grammar.Cast) (int, int) {
	if !prog.RecordsSourceInfo {
		return 0, 0
	}
	if pos := t.Diagnostic(); pos.IsValid() {
		return pos.Line, pos.Col
	}
	return 0, 0
}
