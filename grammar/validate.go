package grammar

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ValidationError describes a structural defect in a grammar AST. Path uses
// JSON field names and zero-based array indexes. Pos is the closest known
// source position; JSON grammars do not carry positions.
type ValidationError struct {
	Path string
	Pos  Pos
	Msg  string
}

func (e *ValidationError) Error() string {
	message := e.Path + ": " + e.Msg
	if e.Pos.IsValid() {
		return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Col, message)
	}
	return message
}

type validationFrame struct {
	node  any
	field string
	index int
	exit  bool
}

// Validate checks required children, nonnegative positional references,
// repetition bounds and pointer cycles without mutating g. It returns the
// first *ValidationError in deterministic depth-first order, checking a node's
// bounds before its children, or nil.
//
// Optional children may be nil, but an interface containing a nil pointer is
// invalid. Shared subtrees are allowed. Empty collections and any negative
// Repeat.Max retain their existing meanings. Source layout is not checked.
// Name resolution, types, attributes and Pratt semantics are checked by the
// compiler. Validation does not impose a size or depth limit.
func Validate(g *Grammar) error {
	stack := []validationFrame{{node: g, index: -1}}
	var path []validationFrame
	seen := make(map[any]uint8)
	fail := func(field, message string) error {
		var b strings.Builder
		b.WriteByte('$')
		pos := Pos{}
		for _, f := range path {
			if f.field != "" {
				b.WriteByte('.')
				b.WriteString(f.field)
			}
			if f.index >= 0 {
				b.WriteByte('[')
				b.WriteString(strconv.Itoa(f.index))
				b.WriteByte(']')
			}
			// Only error paths inspect positions; successful validation does not
			// reflect over fields or allocate a string for every visited node.
			v := reflect.ValueOf(f.node)
			if v.IsValid() && v.Kind() == reflect.Pointer && !v.IsNil() && v.Type().Elem().PkgPath() == reflect.TypeFor[Grammar]().PkgPath() {
				if p := v.Elem().FieldByName("Pos"); p.IsValid() {
					if p, ok := p.Interface().(Pos); ok && p.IsValid() {
						pos = p
					}
				}
			}
		}
		if field != "" {
			b.WriteByte('.')
			b.WriteString(field)
		}
		return &ValidationError{Path: b.String(), Pos: pos, Msg: message}
	}
	push := func(node any, field string, index int, optional bool) {
		if optional && node == nil {
			return
		}
		stack = append(stack, validationFrame{node: node, field: field, index: index})
	}
	for len(stack) != 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.exit {
			seen[f.node] = 2
			path = path[:len(path)-1]
			continue
		}
		path = append(path, f)
		if f.node == nil {
			return fail("", "required node is nil")
		}
		v := reflect.ValueOf(f.node)
		if v.Kind() != reflect.Pointer {
			return fail("", fmt.Sprintf("unsupported node %T", f.node))
		}
		if v.IsNil() {
			return fail("", "required node is nil")
		}
		// Leaf nodes need no cycle tracking. Non-leaf nodes stay active until
		// their exit frame, distinguishing a pointer cycle from a shared child.
		switch n := f.node.(type) {
		case *TypeRef, *TerminalSpec, *Ref, *Literal, *CharClass, *Any,
			*Cut, *Top, *Bottom, *BeginInput, *EndInput, *BeginLine, *EndLine,
			*IntLit, *StringLit, *BoolLit, *NilLit, *CaptureRef, *VarRef:
			path = path[:len(path)-1]
			continue
		case *IndexRef:
			if n.Index < 0 {
				return fail("index", "positional reference must be nonnegative")
			}
			path = path[:len(path)-1]
			continue
		case *Repeat:
			if n.Min < 0 {
				return fail("min", "repetition minimum must be nonnegative")
			}
			if n.Max >= 0 && n.Max < n.Min {
				return fail("max", "repetition maximum is less than minimum")
			}
		}
		if seen[f.node] == 1 {
			return fail("", "pointer cycle in grammar AST")
		}
		if seen[f.node] == 2 {
			path = path[:len(path)-1]
			continue
		}
		seen[f.node] = 1
		stack = append(stack, validationFrame{node: f.node, exit: true})
		// Push children in reverse order so diagnostics visit source order.
		switch n := f.node.(type) {
		case *Grammar:
			for i := len(n.Statements) - 1; i >= 0; i-- {
				push(n.Statements[i], "statements", i, false)
			}
		case *TypeDef:
			push(n.Spec, "spec", -1, false)
		case *RuleDef:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
			push(n.Type, "type", -1, true)
		case *StructSpec:
			for i := len(n.Fields) - 1; i >= 0; i-- {
				push(n.Fields[i], "fields", i, false)
			}
		case *Field:
			push(n.Type, "type", -1, false)
		case *AliasSpec:
			push(n.Type, "type", -1, false)
		case *ListType:
			push(n.Elem, "elem", -1, false)
		case *OptionalType:
			push(n.Elem, "elem", -1, false)
		case *UnionType:
			for i := len(n.Types) - 1; i >= 0; i-- {
				push(n.Types[i], "types", i, false)
			}
		case *Seq:
			for i := len(n.Items) - 1; i >= 0; i-- {
				push(n.Items[i], "items", i, false)
			}
		case *Choice:
			for i := len(n.Alts) - 1; i >= 0; i-- {
				push(n.Alts[i], "alts", i, false)
			}
		case *Repeat:
			push(n.Expr, "expr", -1, false)
		case *Optional:
			push(n.Expr, "expr", -1, false)
		case *And:
			push(n.Expr, "expr", -1, false)
		case *Not:
			push(n.Expr, "expr", -1, false)
		case *Atomic:
			push(n.Expr, "expr", -1, false)
		case *Discard:
			push(n.Expr, "expr", -1, false)
		case *Capture:
			push(n.Expr, "expr", -1, false)
		case *Predicate:
			push(n.Term, "term", -1, false)
		case *Attributed:
			for i := len(n.Attrs) - 1; i >= 0; i-- {
				push(n.Attrs[i], "attrs", i, false)
			}
			push(n.Expr, "expr", -1, false)
		case *Attribute:
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, false)
			}
		case *AttrArg:
			push(n.Value, "value", -1, false)
		case *Pratt:
			for i := len(n.Levels) - 1; i >= 0; i-- {
				push(n.Levels[i], "levels", i, false)
			}
			for i := len(n.Operands) - 1; i >= 0; i-- {
				push(n.Operands[i], "operands", i, false)
			}
			push(n.Skip, "skip", -1, true)
		case *PrattOperand:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
		case *PrattLevel:
			for i := len(n.Operators) - 1; i >= 0; i-- {
				push(n.Operators[i], "operators", i, false)
			}
		case *PrattOperator:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
		case *Member:
			push(n.X, "x", -1, false)
		case *New:
			for i := len(n.Fields) - 1; i >= 0; i-- {
				push(n.Fields[i], "fields", i, false)
			}
		case *FieldInit:
			push(n.Value, "value", -1, false)
		case *Call:
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, false)
			}
		case *Lambda:
			push(n.Body, "body", -1, false)
		case *Binary:
			push(n.R, "r", -1, false)
			push(n.L, "l", -1, false)
		case *Unary:
			push(n.X, "x", -1, false)
		case *Assign:
			push(n.Value, "value", -1, false)
		default:
			return fail("", fmt.Sprintf("unsupported node %T", n))
		}
	}
	return nil
}
