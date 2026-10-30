package core

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// Values of actions and predicates are *Node, int, string, bool, nil, or *closure.
// Nodes created by actions are not fresh, so they get no rule name.

// Function is a function (lambda) passed to a built-in function: *closure when evaluating the
// AST, *vmFunc in bytecode.
type Function interface {
	Arity() int
	// Apply calls the function with the arguments x and y; y is ignored by a function of one
	// parameter. (Arguments are not passed as a slice, which would escape through the interface.)
	Apply(x, y any) (any, error)
}

// Evaluator evaluates an action expression (it differs per backend).
type Evaluator func(ctx *EvalCtx) (any, error)

type Local struct {
	Name string
	Val  any
	Next *Local
}

// EvalCtx is the context needed to evaluate value expressions.
type EvalCtx struct {
	P          *Parser
	Scope      *compile.Scope
	Frame      *Frame
	Items      []*value.Node // elements referenced by $n
	Start, End int           // range matched by the rule
	Locals     *Local        // lambda arguments and OperatorsRule's $lhs, $rhs, $op
	Cbase      int           // start, in p.Created, of the nodes created by this evaluation
}

// runAction evaluates the rule's action.
func (p *Parser) runAction(r *Rule, f *Frame, items []*value.Node, start, end int) *value.Node {
	ctx := p.UseCtx(EvalCtx{P: p, Scope: r.Scope, Frame: f, Items: items, Start: start, End: end, Cbase: len(p.Created)})
	return p.ActionResult(ctx, r.Act, r.WhereName())
}

func (p *Parser) ActionResult(ctx *EvalCtx, act Evaluator, where string) *value.Node {
	v, err := act(ctx)
	n, ok := v.(*value.Node)
	if err == nil && n != nil {
		for _, c := range p.Created[ctx.Cbase:] {
			p.Spend()
			if c == n {
				value.D(n).Start, value.D(n).End = int32(ctx.Start), int32(ctx.End)
				break
			}
		}
	}
	p.DropCreated(ctx.Cbase)
	if err != nil {
		p.FailAction(where, err)
	}
	if v == nil {
		return nil
	}
	if !ok {
		if _, isFn := v.(Function); isFn {
			p.FailAction(where, p.ActionError("result must be a value, got a function"))
		}
		// A value other than a node travels boxed, with the range of the action's input.
		return p.NewNode(value.Data{Kind: value.BoxKind, Box: v, Start: int32(ctx.Start), End: int32(ctx.End)})
	}
	if n != nil {
		// The value of an action is final: a node it returns from the rule body (=> $1) is not
		// labeled by the rules that receive it, which might share it through the memo.
		value.D(n).Fresh = false
	}
	return n
}

// DropCreated releases tracking references without changing the created values.
func (p *Parser) DropCreated(base int) {
	ClearSpend(p, p.Created[base:])
	p.Created = p.Created[:base]
}

func TypeName(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case *value.Node:
		if v == nil {
			return "nil"
		}
		if value.D(v).Kind == value.BoxKind {
			return TypeName(value.D(v).Box)
		}
		if value.D(v).Kind.IsNodeType() {
			return v.Type()
		}
		return value.ValueTypeName(value.ShapeOf(v), v.Type(), len(value.D(v).Children))
	case int:
		return "int"
	case string:
		return "string"
	case bool:
		return "bool"
	case Function:
		return "function"
	}
	return fmt.Sprintf("%T", v)
}

// Member reads x.name.
func (c *EvalCtx) Member(x any, name string) (any, error) {
	n, ok := x.(*value.Node)
	if !ok || n == nil {
		return nil, c.P.ActionError("cannot access .", name, " of ", TypeName(x))
	}
	if value.IsNode(n) {
		switch name {
		case "Start":
			return int(value.D(n).Start), nil
		case "End":
			return int(value.D(n).End), nil
		case "Value":
			return c.P.nodeValue(n), nil
		}
	}
	if capture, ok := strings.CutPrefix(name, "$"); ok {
		// x.$y reads a capture, which struct values do not have; an unmatched capture is nil.
		if _, ok := c.P.Prog.Types[n.Type()].(*grammar.TypeDef); ok {
			return nil, c.P.ActionError(n.Type(), " has no capture ", capture)
		}
		v, _ := c.P.ValueField(value.D(n).Fields, capture)
		return value.UnboxValue(v), nil
	}
	if st, ok := c.P.Prog.Types[n.Type()].(*grammar.TypeDef); ok && !c.P.HasValueField(st, name) {
		return nil, c.P.ActionError(n.Type(), " has no field ", name)
	}
	// Unset fields and unmatched captures are nil.
	v, _ := c.P.ValueField(value.D(n).Fields, name)
	return v, nil
}

// NewStruct creates a struct node. Its range covers the ranges of the fields whose values are
// nodes, or is the rule's range if there are none.
func (c *EvalCtx) NewStruct(typ string, names []string, vals []any) (*value.Node, error) {
	n := c.P.NewNode(value.Data{Kind: c.P.Prog.TypeKind(typ), Fields: c.P.fields(len(names))})
	first := true
	for i, name := range names {
		c.P.Spend()
		v := vals[i]
		if _, isFn := v.(Function); isFn {
			return nil, c.P.ActionError("field ", name, ": cannot store a function")
		}
		c.P.SetValueField(&value.D(n).Fields, name, v)
		if child, ok := v.(*value.Node); ok && child != nil {
			if first || value.D(child).Start < value.D(n).Start {
				value.D(n).Start = value.D(child).Start
			}
			if first || value.D(child).End > value.D(n).End {
				value.D(n).End = value.D(child).End
			}
			first = false
		}
	}
	if first {
		value.D(n).Start, value.D(n).End = int32(c.Start), int32(c.End)
	}
	c.P.Created = append(c.P.Created, n)
	return n, nil
}

func (p *Parser) ValueUnary(op string, x any) (any, error) {
	switch op {
	case "-":
		if i, ok := x.(int); ok {
			return -i, nil
		}
	case "!":
		if b, ok := x.(bool); ok {
			return !b, nil
		}
	}
	if BudgetControlsEnabled && p != nil && p.Control != nil {
		return nil, p.ActionError("invalid operand ", TypeName(x), " for unary ", op)
	}
	return nil, fmt.Errorf("invalid operand %s for unary %s", TypeName(x), op)
}

func (c *EvalCtx) NewList(items []*value.Node) *value.Node {
	kids := c.P.Nodes(len(items))
	CopySpend(c.P, kids, items)
	return c.ListNode(kids)
}

// ListNode returns a list value with the child slice kids (which it takes over). Its range,
// which the value model does not show, covers the elements that are nodes.
func (c *EvalCtx) ListNode(kids []*value.Node) *value.Node {
	return c.valueNode(value.ListValueKind, kids)
}

// TupleNode is ListNode for a tuple value.
func (c *EvalCtx) TupleNode(kids []*value.Node) *value.Node {
	return c.valueNode(value.TupleValueKind, kids)
}

func (c *EvalCtx) valueNode(kind *value.NodeKind, kids []*value.Node) *value.Node {
	n := c.P.NewNode(value.Data{Kind: kind, Children: kids, Start: int32(c.Start), End: int32(c.Start)})
	first := true
	for _, it := range kids {
		c.P.Spend()
		if it == nil {
			continue
		}
		if first || value.D(it).Start < value.D(n).Start {
			value.D(n).Start = value.D(it).Start
		}
		if first || value.D(it).End > value.D(n).End {
			value.D(n).End = value.D(it).End
		}
		first = false
	}
	return n
}

// binaryOp performs a binary operation other than a logical one.
func binaryOp(op string, l, r any) (any, error) {
	return (*Parser)(nil).binaryOperation(op, l, r)
}

func (p *Parser) binaryOperation(op string, l, r any) (any, error) {
	switch op {
	case "[]":
		return p.index(l, r)
	case "==":
		return equal(l, r), nil
	case "!=":
		return !equal(l, r), nil
	}
	switch l := l.(type) {
	case int:
		if r, ok := r.(int); ok {
			switch op {
			case "+":
				return l + r, nil
			case "-":
				return l - r, nil
			case "*":
				return l * r, nil
			case "/", "%":
				if r == 0 {
					return nil, p.ActionError("division by zero")
				}
				if op == "/" {
					return l / r, nil
				}
				return l % r, nil
			case "<":
				return l < r, nil
			case "<=":
				return l <= r, nil
			case ">":
				return l > r, nil
			case ">=":
				return l >= r, nil
			}
		}
	case string:
		if r, ok := r.(string); ok {
			switch op {
			case "+":
				return l + r, nil
			case "<":
				return l < r, nil
			case "<=":
				return l <= r, nil
			case ">":
				return l > r, nil
			case ">=":
				return l >= r, nil
			}
		}
	}
	if BudgetControlsEnabled && p != nil && p.Control != nil {
		return nil, p.ActionError("invalid operands ", TypeName(l), " and ", TypeName(r), " for ", op)
	}
	return nil, fmt.Errorf("invalid operands %s and %s for %s", TypeName(l), TypeName(r), op)
}

func equal(a, b any) bool {
	switch a := a.(type) {
	case *value.Node:
		if a == nil {
			return b == nil
		}
		bn, ok := b.(*value.Node)
		return ok && a == bn
	case nil:
		if bn, ok := b.(*value.Node); ok {
			return bn == nil
		}
		return b == nil
	case int, string, bool:
		return a == b
	}
	return false
}

// Builtin calls the built-in function fn with evaluated arguments.
func (c *EvalCtx) Builtin(fn string, args []any) (any, error) {
	switch fn {
	case "len":
		switch x := args[0].(type) {
		case string:
			return c.P.ValueTextLen(x), nil
		case *value.Node:
			if x == nil {
				return 0, nil
			}
			if value.D(x).Terminal {
				return c.P.ValueTextLen(value.D(x).Text), nil
			}
			return len(value.D(x).Children), nil
		case nil:
			return 0, nil
		}
	case "text":
		s, err := c.TextString(args[0])
		if err != nil {
			return nil, err
		}
		return s, nil
	case "foldl", "foldr":
		f, ok := args[2].(Function)
		if !ok || f.Arity() != 2 {
			return nil, c.P.ActionError(fn, ": third argument must be a function of two parameters")
		}
		items, err := c.P.ValueListItems(fn, args[1])
		if err != nil {
			return nil, err
		}
		acc := args[0]
		for i := range items {
			c.P.Spend()
			it := items[i]
			if fn == "foldr" {
				it = items[len(items)-1-i]
			}
			if acc, err = f.Apply(acc, value.UnboxNode(it)); err != nil {
				return nil, err
			}
		}
		return acc, nil
	case "map", "list", "concat", "filter":
		// The elements are gathered on KidStack; lambdas that build lists push above them.
		base := len(c.P.KidStack)
		if err := c.PushElems(fn, args); err != nil {
			c.P.DropKids(base)
			return nil, err
		}
		return c.ListNode(c.P.Kids(base)), nil
	default:
		return nil, c.P.ActionError("unknown function ", fn)
	}
	return nil, c.P.ActionError(fn, ": invalid argument ", TypeName(args[0]))
}

// TextString returns text(v) (the built-in) as a string.
func (c *EvalCtx) TextString(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case *value.Node:
		if v == nil {
			return "", nil
		}
		if value.D(v).Terminal {
			return value.D(v).Text, nil
		}
		return c.P.Text(int(value.D(v).Start), int(value.D(v).End)), nil
	}
	return "", c.P.ActionError("text: invalid argument ", TypeName(v))
}

// PushElems pushes onto KidStack the elements of the list that the list built-in fn (map, list
// or concat) makes from args.
func (c *EvalCtx) PushElems(fn string, args []any) error {
	switch fn {
	case "filter":
		return c.pushFiltered(args)
	case "map":
		f, ok := args[1].(Function)
		if !ok || f.Arity() != 1 {
			return c.P.ActionError("map: second argument must be a function of one parameter")
		}
		items, err := c.P.ValueListItems("map", args[0])
		if err != nil {
			return err
		}
		for _, it := range items {
			c.P.Spend()
			v, err := f.Apply(value.UnboxNode(it), nil)
			if err != nil {
				return err
			}
			n, err := c.Element("map", v)
			if err != nil {
				return err
			}
			c.P.KidStack = append(c.P.KidStack, n)
		}
	case "list":
		for _, a := range args {
			c.P.Spend()
			n, err := c.Element("list", a)
			if err != nil {
				return err
			}
			c.P.KidStack = append(c.P.KidStack, n)
		}
	case "concat":
		for _, a := range args {
			c.P.Spend()
			items, err := c.P.ValueListItems("concat", a)
			if err != nil {
				return err
			}
			c.P.KidStack = AppendSpend(c.P, c.P.KidStack, items)
		}
	}
	return nil
}

// index returns element i of the list l.
func (p *Parser) index(l, i any) (any, error) {
	items, err := p.ValueListItems("[]", l)
	if err != nil {
		return nil, err
	}
	n, ok := i.(int)
	if !ok {
		return nil, p.ActionError("index must be int, got ", TypeName(i))
	}
	if n < 0 || n >= len(items) {
		return nil, p.ActionError("index ", strconv.Itoa(n), " out of range for ", strconv.Itoa(len(items)), " elements")
	}
	return value.UnboxNode(items[n]), nil
}

func (p *Parser) ValueListItems(fn string, v any) ([]*value.Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *value.Node:
		if x == nil {
			return nil, nil
		}
		return value.D(x).Children, nil
	}
	if BudgetControlsEnabled && p != nil && p.Control != nil {
		return nil, p.ActionError(fn, ": expected a list, got ", TypeName(v))
	}
	return nil, fmt.Errorf("%s: expected a list, got %s", fn, TypeName(v))
}

// Element returns v as an element of a list that the list built-in fn makes. An int, string or
// bool travels boxed, with the range of the action's input, as the result of an action does.
func (c *EvalCtx) Element(fn string, v any) (*value.Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *value.Node:
		return x, nil
	case int, string, bool:
		return c.P.NewNode(value.Data{Kind: value.BoxKind, Box: x, Start: int32(c.Start), End: int32(c.End)}), nil
	}
	return nil, c.P.ActionError(fn, ": elements must be values, got ", TypeName(v))
}

// Keep returns c, or a copy of c on the heap if it belongs to a lambda call (closure.apply),
// whose context and parameters are freed when the call returns while a lambda created in it can
// be returned from it.
func (c *EvalCtx) Keep() *EvalCtx {
	if c == &c.P.ectx {
		return c
	}
	k := *c
	var head *Local
	for l, link := c.Locals, &head; l != nil; l = l.Next {
		c.P.Spend()
		*link = &Local{Name: l.Name, Val: l.Val}
		link = &(*link).Next
	}
	k.Locals = head
	return &k
}
