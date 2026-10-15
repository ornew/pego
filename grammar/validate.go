package grammar

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ValidationError describes a structural defect in a grammar AST. Path uses
// JSON field names and zero-based array indexes. Pos is the closest known
// diagnostic position, when the AST carries source information.
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
// repetition and character-range bounds, and pointer cycles without mutating g.
// It returns the first *ValidationError in deterministic depth-first order,
// checking a node's bounds before its children, or nil.
//
// Optional children may be nil, but an interface containing a nil pointer is
// invalid. Shared subtrees are allowed. Empty collections and any negative
// Repeat.Max retain their existing meanings. Source layout is not checked.
// Name resolution, types, attributes and OperatorsRule semantics are checked by the
// compiler. Validation does not impose a size or depth limit.
func Validate(g *Grammar) error {
	return validate(g, nil)
}

// ValidateContext checks the same grammar invariants as Validate, but stops
// without returning partial diagnostics when ctx is cancelled. It polls while
// traversing the AST; individual string checks and sorting are not interruptible.
func ValidateContext(ctx context.Context, g *Grammar) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ctx.Done() == nil {
		return Validate(g)
	}
	control := &validationControl{ctx: ctx}
	defer func() {
		if failure := recover(); failure != nil {
			if cancelled, ok := failure.(validationCancellation); ok {
				err = cancelled.err
			} else {
				panic(failure)
			}
		}
	}()
	err = validate(g, control)
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	return err
}

type validationCancellation struct{ err error }

type validationControl struct {
	ctx   context.Context
	steps uint32
}

func (c *validationControl) poll() {
	if c == nil {
		return
	}
	c.steps++
	if c.steps%256 == 0 {
		c.check()
	}
}

func (c *validationControl) check() {
	if err := c.ctx.Err(); err != nil {
		panic(validationCancellation{err})
	}
}

func validate(g *Grammar, control *validationControl) error {
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
				if n, ok := f.node.(SourceNode); ok && n.Source().Diagnostic().IsValid() {
					pos = n.Source().Diagnostic()
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
		control.poll()
		if optional && node == nil {
			return
		}
		stack = append(stack, validationFrame{node: node, field: field, index: index})
	}
	for len(stack) != 0 {
		control.poll()
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
		switch f.node.(type) {
		case int, string, bool:
			path = path[:len(path)-1]
			continue
		}
		v := reflect.ValueOf(f.node)
		if v.Kind() != reflect.Pointer {
			return fail("", fmt.Sprintf("unsupported node %T", f.node))
		}
		if v.IsNil() {
			return fail("", "required node is nil")
		}
		if field, name := invalidIdentifier(f.node, control); field != "" {
			if name != "" && name[0] == '_' {
				return fail(field, "identifier "+name+" is reserved")
			}
			return fail(field, fmt.Sprintf("invalid identifier %q: use ASCII letters, digits and underscores", name))
		}
		// Leaf nodes need no cycle tracking. Non-leaf nodes stay active until
		// their exit frame, distinguishing a pointer cycle from a shared child.
		switch n := f.node.(type) {
		case *TypeRef, *TerminalSpec, *Ref, *Literal, *Any,
			*Cut, *Top, *Bottom, *BeginInput, *EndInput, *BeginLine, *EndLine,
			*IntLit, *StringLit, *BoolLit, *NilLit, *CaptureRef, *VarRef:
			path = path[:len(path)-1]
			continue
		case *CharClass:
			for start := 0; start < len(n.Ranges); {
				end := len(n.Ranges)
				if control != nil {
					control.check()
					end = start + min(256, end-start)
				}
				// The ordinary path validates the full slice without a polling
				// branch per range; controlled validation checks each chunk.
				for offset, r := range n.Ranges[start:end] {
					i := start + offset
					if !utf8.ValidRune(r.Lo) {
						return fail("ranges["+strconv.Itoa(i)+"].lo", "character range lower bound must be a Unicode scalar value")
					}
					if !utf8.ValidRune(r.Hi) {
						return fail("ranges["+strconv.Itoa(i)+"].hi", "character range upper bound must be a Unicode scalar value")
					}
					if r.Hi < r.Lo {
						return fail("ranges["+strconv.Itoa(i)+"].hi", "character range upper bound is less than lower bound")
					}
				}
				start = end
			}
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
		case *RuleDef:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
			}
		case *OperatorsRuleOperand:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
			}
		case *OperatorsRuleOperator:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
			}
		case *InlineAction:
			if n.DefaultAction && n.Action != nil {
				return fail("action", "default action cannot also contain an action expression")
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
		case *TypeGroup:
			push(n.Type, "type", -1, false)
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
		case *Group:
			push(n.Expr, "expr", -1, false)
		case *InlineAction:
			push(n.Action, "action", -1, n.DefaultAction)
			push(n.Expr, "expr", -1, false)
		case *Optional:
			push(n.Expr, "expr", -1, false)
		case *And:
			push(n.Expr, "expr", -1, false)
		case *Not:
			push(n.Expr, "expr", -1, false)
		case *Atomic:
			push(n.Expr, "expr", -1, false)
		case *Recovery:
			push(n.Skip, "skip", -1, false)
			push(n.Expr, "expr", -1, false)
		case *Stream:
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
			keys := make([]string, 0, len(n.KwArgs))
			for key := range n.KwArgs {
				control.poll()
				keys = append(keys, key)
			}
			sort.Strings(keys)
			validValue := func(value any) bool {
				switch value.(type) {
				case nil, int, string, bool, Expr:
					return true
				}
				return false
			}
			for _, key := range keys {
				control.poll()
				if !validValue(n.KwArgs[key]) {
					return fail("kwArgs["+strconv.Quote(key)+"]", "attribute value must be a scalar or parser expression")
				}
			}
			for i, value := range n.Args {
				control.poll()
				if !validValue(value) {
					return fail("args["+strconv.Itoa(i)+"]", "attribute value must be a scalar or parser expression")
				}
			}
			for i := len(keys) - 1; i >= 0; i-- {
				key := keys[i]
				push(n.KwArgs[key], "kwArgs["+strconv.Quote(key)+"]", -1, true)
			}
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, true)
			}
		case *OperatorsRule:
			for i := len(n.Levels) - 1; i >= 0; i-- {
				push(n.Levels[i], "levels", i, false)
			}
			for i := len(n.Operands) - 1; i >= 0; i-- {
				push(n.Operands[i], "operands", i, false)
			}
			push(n.Skip, "skip", -1, true)
		case *OperatorsRuleOperand:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
		case *OperatorsRuleLevel:
			for i := len(n.Operators) - 1; i >= 0; i-- {
				push(n.Operators[i], "operators", i, false)
			}
		case *OperatorsRuleOperator:
			push(n.Action, "action", -1, true)
			push(n.Expr, "expr", -1, false)
		case *Block:
			for i := len(n.Statements) - 1; i >= 0; i-- {
				push(n.Statements[i], "statements", i, false)
			}
		case *Member:
			push(n.X, "x", -1, false)
		case *TermGroup:
			push(n.Term, "term", -1, false)
		case *New:
			for i := len(n.Fields) - 1; i >= 0; i-- {
				push(n.Fields[i], "fields", i, false)
			}
		case *ListConstructor:
			for i := len(n.Items) - 1; i >= 0; i-- {
				push(n.Items[i], "items", i, false)
			}
			push(n.Elem, "elem", -1, false)
		case *TerminalConstructor:
			for i := len(n.Args) - 1; i >= 0; i-- {
				push(n.Args[i], "args", i, false)
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

// invalidIdentifier checks source names without changing literal data or
// treating the Top node's underscore spelling as an identifier. Empty optional
// names (package and Operators Rule level) are valid.
func invalidIdentifier(node any, control *validationControl) (field, name string) {
	invalid := func(s string) bool {
		if s == "" {
			return true
		}
		if s[0] == '_' {
			return true
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			if i == 0 && c >= '0' && c <= '9' ||
				!(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
				return true
			}
		}
		return false
	}
	switch n := node.(type) {
	case *Grammar:
		if n.Package == "" {
			return "", ""
		}
		field, name = "package", n.Package
	case *RuleDef:
		field, name = "name", n.Name
	case *TypeDef:
		field, name = "name", n.Name
	case *TypeRef:
		field, name = "name", n.Name
	case *Field:
		field, name = "name", n.Name
	case *Ref:
		if invalid(n.Name) {
			return "name", n.Name
		}
		if n.Level == "" {
			return "", ""
		}
		field, name = "level", n.Level
	case *Capture:
		field, name = "name", n.Name
	case *CaptureRef:
		field, name = "name", n.Name
	case *VarRef:
		field, name = "name", n.Name
	case *Assign:
		field, name = "name", n.Name
	case *Member:
		field, name = "name", n.Name
	case *New:
		field, name = "type", n.Type
	case *TerminalConstructor:
		field, name = "type", n.Type
	case *FieldInit:
		field, name = "name", n.Name
	case *Call:
		field, name = "func", n.Func
	case *OperatorsRuleLevel:
		if n.Name == "" {
			return "", ""
		}
		field, name = "name", n.Name
	case *Attribute:
		if invalid(n.Name) {
			return "name", n.Name
		}
		bad := ""
		found := false
		for key := range n.KwArgs {
			control.poll()
			if invalid(key) && (!found || key < bad) {
				bad = key
				found = true
			}
		}
		if found {
			return "kwArgs[" + strconv.Quote(bad) + "]", bad
		}
	case *Lambda:
		for i, param := range n.Params {
			control.poll()
			if invalid(param) {
				return "params[" + strconv.Itoa(i) + "]", param
			}
		}
	}
	if invalid(name) {
		return field, name
	}
	return "", ""
}
