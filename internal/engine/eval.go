package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/value"
)

// Values of actions and predicates are *Node, int, string, bool, nil, or *closure.
// Nodes created by actions are not fresh, so they get no rule name.

// function is a function (lambda) passed to a built-in function: *closure when evaluating the
// AST, *vmFunc in bytecode.
type function interface {
	arity() int
	// apply calls the function with the arguments x and y; y is ignored by a function of one
	// parameter. (Arguments are not passed as a slice, which would escape through the interface.)
	apply(x, y any) (any, error)
}

// evaluator evaluates an action expression (it differs per backend).
type evaluator func(ctx *evalCtx) (any, error)

type closure struct {
	params []string
	body   grammar.Term
	ctx    *evalCtx
}

func (f *closure) arity() int { return len(f.params) }

type local struct {
	name string
	val  any
	next *local
}

// evalCtx is the context needed to evaluate value expressions.
type evalCtx struct {
	p          *parser
	scope      *scope
	frame      *frame
	items      []*value.Node // elements referenced by $n
	start, end int           // range matched by the rule
	locals     *local        // lambda arguments and OperatorsRule's $lhs, $rhs, $op
	cbase      int           // start, in p.created, of the nodes created by this evaluation
}

// runAction evaluates the rule's action.
func (p *parser) runAction(r *rule, f *frame, items []*value.Node, start, end int) *value.Node {
	ctx := p.useCtx(evalCtx{p: p, scope: r.scope, frame: f, items: items, start: start, end: end, cbase: len(p.created)})
	return p.actionResult(ctx, r.act, r.whereName())
}

// termEvaluator returns an evaluator for an AST value expression.
func termEvaluator(t grammar.Term) evaluator {
	return func(ctx *evalCtx) (any, error) { return ctx.eval(t) }
}

func (p *parser) actionResult(ctx *evalCtx, act evaluator, where string) *value.Node {
	v, err := act(ctx)
	n, ok := v.(*value.Node)
	if err == nil && n != nil {
		for _, c := range p.created[ctx.cbase:] {
			p.stepWork()
			if c == n {
				value.D(n).Start, value.D(n).End = int32(ctx.start), int32(ctx.end)
				break
			}
		}
	}
	p.dropCreated(ctx.cbase)
	if err != nil {
		p.failAction(where, err)
	}
	if v == nil {
		return nil
	}
	if !ok {
		if _, isFn := v.(function); isFn {
			p.failAction(where, p.actionError("result must be a value, got a function"))
		}
		// A value other than a node travels boxed, with the range of the action's input.
		return p.newNode(value.Data{Kind: value.BoxKind, Box: v, Start: int32(ctx.start), End: int32(ctx.end)})
	}
	if n != nil {
		// The value of an action is final: a node it returns from the rule body (=> $1) is not
		// labeled by the rules that receive it, which might share it through the memo.
		value.D(n).Fresh = false
	}
	return n
}

// dropCreated releases tracking references without changing the created values.
func (p *parser) dropCreated(base int) {
	clearWork(p, p.created[base:])
	p.created = p.created[:base]
}

func typeName(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case *value.Node:
		if v == nil {
			return "nil"
		}
		if value.D(v).Kind == value.BoxKind {
			return typeName(value.D(v).Box)
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
	case function:
		return "function"
	}
	return fmt.Sprintf("%T", v)
}

// convert evaluates the node type constructor t, T{x}: it converts the value of x to the named
// node type T.
func (c *evalCtx) convert(t *grammar.New) (any, error) {
	x, err := c.eval(t.Args[0])
	if err != nil {
		return nil, err
	}
	p := c.p
	ks := p.prog.nodeKinds[t.Type]
	if ks == nil {
		return nil, p.convertError(x, t.Type)
	}
	name := ks[0].Type // the type's own name, also for an alias of it
	n, ok := ks.Convert(x)
	if !ok {
		return nil, p.convertError(x, name)
	}
	return n, nil
}

func (c *evalCtx) eval(t grammar.Term) (any, error) {
	c.p.stepWork()
	switch t := t.(type) {
	case *grammar.IntLit:
		return t.Value, nil
	case *grammar.StringLit:
		return t.Value, nil
	case *grammar.BoolLit:
		return t.Value, nil
	case *grammar.NilLit:
		return nil, nil

	case *grammar.CaptureRef:
		for l := c.locals; l != nil; l = l.next {
			c.p.stepWork()
			if c.p.valueEqual(l.name, t.Name) {
				return value.UnboxValue(l.val), nil
			}
		}
		if i, ok := c.scope.slots[t.Name]; ok && c.frame != nil {
			return value.UnboxNode(c.frame.vals[i]), nil
		}
		return nil, c.p.actionError("undefined capture $", t.Name)

	case *grammar.IndexRef:
		return nil, c.p.actionError("positional captures are removed")

	case *grammar.VarRef:
		v, ok := c.p.lookupValue(t.Name)
		if !ok {
			return nil, c.p.actionError("variable ", t.Name, " is not defined")
		}
		return v, nil

	case *grammar.Member:
		x, err := c.eval(t.X)
		if err != nil {
			return nil, err
		}
		return c.member(x, t.Name)

	case *grammar.New:
		if len(t.Args) != 0 {
			return c.convert(t)
		}
		// The values are kept on the expression stack (evaluations nested in them push above), and
		// the names in a buffer filled once no evaluation is pending; newStruct retains neither.
		p := c.p
		base := len(p.estack)
		for _, fi := range t.Fields {
			p.stepWork()
			v, err := c.eval(fi.Value)
			if err != nil {
				p.estack = p.estack[:base]
				return nil, err
			}
			p.estack = append(p.estack, v)
		}
		p.names = p.names[:0]
		for _, fi := range t.Fields {
			p.stepWork()
			p.names = append(p.names, fi.Name)
		}
		n, err := c.newStruct(t.Type, p.names, p.estack[base:])
		p.estack = p.estack[:base]
		return n, err
	case *grammar.ListConstructor:
		return c.evalList(t.Items)
	case *grammar.TupleLit:
		return c.evalTuple(t.Elems)

	case *grammar.Call:
		if c.p.prog.projected[t] {
			// map($x, (e) => e.$f) of a projected repetition: $x holds the values already.
			x, err := c.eval(t.Args[0])
			if err != nil {
				return nil, err
			}
			items, err := c.p.valueListItems("map", x)
			if err != nil {
				return nil, err
			}
			return c.newList(items), nil
		}
		if t.Func == "concat" && c.p.fusable(t) {
			base := len(c.p.kidStack)
			for _, a := range t.Args {
				c.p.stepWork()
				if err := c.pushConcatArg(a); err != nil {
					c.p.dropKids(base)
					return nil, err
				}
			}
			return c.listNode(c.p.kids(base)), nil
		}
		// The arguments are kept on the expression stack, as in vmProgram.eval: lambdas called by the
		// built-in push above them, and built-ins do not retain args.
		p := c.p
		base := len(p.estack)
		for _, a := range t.Args {
			c.p.stepWork()
			v, err := c.eval(a)
			if err != nil {
				p.estack = p.estack[:base]
				return nil, err
			}
			p.estack = append(p.estack, v)
		}
		v, err := c.builtin(t.Func, p.estack[base:])
		p.estack = p.estack[:base]
		return v, err

	case *grammar.Lambda:
		f := c.p.closures.alloc()
		*f = closure{params: t.Params, body: t.Body, ctx: c.keep()}
		return f, nil

	case *grammar.Unary:
		x, err := c.eval(t.X)
		if err != nil {
			return nil, err
		}
		return c.p.valueUnary(t.Op, x)

	case *grammar.Cast:
		x, err := c.eval(t.X)
		if err != nil {
			return nil, err
		}
		if ft := c.p.prog.castType(t); ft != nil && !c.p.fieldConforms(ft, x) {
			line, col := c.p.prog.castPos(t)
			return nil, c.p.castError(x, ft, c.start, line, col)
		}
		return x, nil

	case *grammar.Binary:
		return c.binary(t)

	case *grammar.Bind:
		return nil, c.p.actionError("binding ", t.Name, " := ... is only allowed as a whole predicate")
	}
	return nil, fmt.Errorf("unsupported term %T", t)
}

// evalList shares the existing list builder without allocating a synthetic AST.
func (c *evalCtx) evalList(items []grammar.Term) (any, error) {
	p := c.p
	base := len(p.estack)
	for _, item := range items {
		val, err := c.eval(item)
		if err != nil {
			p.estack = p.estack[:base]
			return nil, err
		}
		p.estack = append(p.estack, val)
	}
	val, err := c.builtin("list", p.estack[base:])
	p.estack = p.estack[:base]
	return val, err
}

// evalTuple evaluates the elements of a tuple literal, left to right, and returns the tuple value.
func (c *evalCtx) evalTuple(elems []grammar.Term) (any, error) {
	p := c.p
	base := len(p.estack)
	for _, e := range elems {
		val, err := c.eval(e)
		if err != nil {
			p.estack = p.estack[:base]
			return nil, err
		}
		p.estack = append(p.estack, val)
	}
	kb := len(p.kidStack)
	for _, v := range p.estack[base:] {
		p.stepWork()
		n, err := c.element("tuple", v)
		if err != nil {
			p.dropKids(kb)
			p.estack = p.estack[:base]
			return nil, err
		}
		p.kidStack = append(p.kidStack, n)
	}
	p.estack = p.estack[:base]
	return c.tupleNode(p.kids(kb)), nil
}

// member reads x.name.
func (c *evalCtx) member(x any, name string) (any, error) {
	n, ok := x.(*value.Node)
	if !ok || n == nil {
		return nil, c.p.actionError("cannot access .", name, " of ", typeName(x))
	}
	if value.IsNode(n) {
		switch name {
		case "Start":
			return int(value.D(n).Start), nil
		case "End":
			return int(value.D(n).End), nil
		case "Value":
			return c.p.nodeValue(n), nil
		}
	}
	if capture, ok := strings.CutPrefix(name, "$"); ok {
		// x.$y reads a capture, which struct values do not have; an unmatched capture is nil.
		if _, ok := c.p.prog.types[n.Type()].(*grammar.TypeDef); ok {
			return nil, c.p.actionError(n.Type(), " has no capture ", capture)
		}
		v, _ := c.p.valueField(value.D(n).Fields, capture)
		return value.UnboxValue(v), nil
	}
	if st, ok := c.p.prog.types[n.Type()].(*grammar.TypeDef); ok && !c.p.hasValueField(st, name) {
		return nil, c.p.actionError(n.Type(), " has no field ", name)
	}
	// Unset fields and unmatched captures are nil.
	v, _ := c.p.valueField(value.D(n).Fields, name)
	return v, nil
}

// newStruct creates a struct node. Its range covers the ranges of the fields whose values are
// nodes, or is the rule's range if there are none.
func (c *evalCtx) newStruct(typ string, names []string, vals []any) (*value.Node, error) {
	n := c.p.newNode(value.Data{Kind: c.p.prog.typeKind(typ), Fields: c.p.fields(len(names))})
	first := true
	for i, name := range names {
		c.p.stepWork()
		v := vals[i]
		if _, isFn := v.(function); isFn {
			return nil, c.p.actionError("field ", name, ": cannot store a function")
		}
		c.p.setValueField(&value.D(n).Fields, name, v)
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
		value.D(n).Start, value.D(n).End = int32(c.start), int32(c.end)
	}
	c.p.created = append(c.p.created, n)
	return n, nil
}

func (p *parser) valueUnary(op string, x any) (any, error) {
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
	if workControlsEnabled && p != nil && p.control != nil {
		return nil, p.actionError("invalid operand ", typeName(x), " for unary ", op)
	}
	return nil, fmt.Errorf("invalid operand %s for unary %s", typeName(x), op)
}

func (c *evalCtx) newList(items []*value.Node) *value.Node {
	kids := c.p.nodes(len(items))
	copyWork(c.p, kids, items)
	return c.listNode(kids)
}

// listNode returns a list value with the child slice kids (which it takes over). Its range,
// which the value model does not show, covers the elements that are nodes.
func (c *evalCtx) listNode(kids []*value.Node) *value.Node {
	return c.valueNode(value.ListValueKind, kids)
}

// tupleNode is listNode for a tuple value.
func (c *evalCtx) tupleNode(kids []*value.Node) *value.Node {
	return c.valueNode(value.TupleValueKind, kids)
}

func (c *evalCtx) valueNode(kind *value.NodeKind, kids []*value.Node) *value.Node {
	n := c.p.newNode(value.Data{Kind: kind, Children: kids, Start: int32(c.start), End: int32(c.start)})
	first := true
	for _, it := range kids {
		c.p.stepWork()
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

func (c *evalCtx) binary(t *grammar.Binary) (any, error) {
	if t.Op == "==" || t.Op == "!=" {
		// Comparing two texts (such as an end tag with its start tag) compares the strings
		// directly: storing a string in an interface would allocate.
		if lx, ok := textArg(t.L); ok {
			if rx, ok := textArg(t.R); ok {
				ls, err := c.textOf(lx)
				if err != nil {
					return nil, err
				}
				rs, err := c.textOf(rx)
				if err != nil {
					return nil, err
				}
				return c.p.valueEqual(ls, rs) == (t.Op == "=="), nil
			}
		}
	}
	l, err := c.eval(t.L)
	if err != nil {
		return nil, err
	}
	// Logical operators short-circuit.
	if t.Op == "&&" || t.Op == "||" {
		lb, ok := l.(bool)
		if !ok {
			return nil, c.p.actionError("invalid operand ", typeName(l), " for ", t.Op)
		}
		if lb == (t.Op == "||") {
			return lb, nil
		}
		r, err := c.eval(t.R)
		if err != nil {
			return nil, err
		}
		rb, ok := r.(bool)
		if !ok {
			return nil, c.p.actionError("invalid operand ", typeName(r), " for ", t.Op)
		}
		return rb, nil
	}
	r, err := c.eval(t.R)
	if err != nil {
		return nil, err
	}
	return c.p.valueBinary(t.Op, l, r)
}

// binaryOp performs a binary operation other than a logical one.
func binaryOp(op string, l, r any) (any, error) {
	return (*parser)(nil).binaryOperation(op, l, r)
}

func (p *parser) binaryOperation(op string, l, r any) (any, error) {
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
					return nil, p.actionError("division by zero")
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
	if workControlsEnabled && p != nil && p.control != nil {
		return nil, p.actionError("invalid operands ", typeName(l), " and ", typeName(r), " for ", op)
	}
	return nil, fmt.Errorf("invalid operands %s and %s for %s", typeName(l), typeName(r), op)
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

// builtin calls the built-in function fn with evaluated arguments.
func (c *evalCtx) builtin(fn string, args []any) (any, error) {
	switch fn {
	case "len":
		switch x := args[0].(type) {
		case string:
			return c.p.valueTextLen(x), nil
		case *value.Node:
			if x == nil {
				return 0, nil
			}
			if value.D(x).Terminal {
				return c.p.valueTextLen(value.D(x).Text), nil
			}
			return len(value.D(x).Children), nil
		case nil:
			return 0, nil
		}
	case "text":
		s, err := c.textString(args[0])
		if err != nil {
			return nil, err
		}
		return s, nil
	case "foldl", "foldr":
		f, ok := args[2].(function)
		if !ok || f.arity() != 2 {
			return nil, c.p.actionError(fn, ": third argument must be a function of two parameters")
		}
		items, err := c.p.valueListItems(fn, args[1])
		if err != nil {
			return nil, err
		}
		acc := args[0]
		for i := range items {
			c.p.stepWork()
			it := items[i]
			if fn == "foldr" {
				it = items[len(items)-1-i]
			}
			if acc, err = f.apply(acc, value.UnboxNode(it)); err != nil {
				return nil, err
			}
		}
		return acc, nil
	case "map", "list", "concat", "filter":
		// The elements are gathered on kidStack; lambdas that build lists push above them.
		base := len(c.p.kidStack)
		if err := c.pushElems(fn, args); err != nil {
			c.p.dropKids(base)
			return nil, err
		}
		return c.listNode(c.p.kids(base)), nil
	default:
		return nil, c.p.actionError("unknown function ", fn)
	}
	return nil, c.p.actionError(fn, ": invalid argument ", typeName(args[0]))
}

// textOf evaluates x and returns text(x) as a string.
func (c *evalCtx) textOf(x grammar.Term) (string, error) {
	v, err := c.eval(x)
	if err != nil {
		return "", err
	}
	return c.textString(v)
}

// textString returns text(v) (the built-in) as a string.
func (c *evalCtx) textString(v any) (string, error) {
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
		return c.p.text(int(value.D(v).Start), int(value.D(v).End)), nil
	}
	return "", c.p.actionError("text: invalid argument ", typeName(v))
}

// pushElems pushes onto kidStack the elements of the list that the list built-in fn (map, list
// or concat) makes from args.
func (c *evalCtx) pushElems(fn string, args []any) error {
	switch fn {
	case "filter":
		return c.pushFiltered(args)
	case "map":
		f, ok := args[1].(function)
		if !ok || f.arity() != 1 {
			return c.p.actionError("map: second argument must be a function of one parameter")
		}
		items, err := c.p.valueListItems("map", args[0])
		if err != nil {
			return err
		}
		for _, it := range items {
			c.p.stepWork()
			v, err := f.apply(value.UnboxNode(it), nil)
			if err != nil {
				return err
			}
			n, err := c.element("map", v)
			if err != nil {
				return err
			}
			c.p.kidStack = append(c.p.kidStack, n)
		}
	case "list":
		for _, a := range args {
			c.p.stepWork()
			n, err := c.element("list", a)
			if err != nil {
				return err
			}
			c.p.kidStack = append(c.p.kidStack, n)
		}
	case "concat":
		for _, a := range args {
			c.p.stepWork()
			items, err := c.p.valueListItems("concat", a)
			if err != nil {
				return err
			}
			c.p.kidStack = appendWork(c.p, c.p.kidStack, items)
		}
	}
	return nil
}

// pushConcatArg pushes onto kidStack the elements of t, an argument of a fusable concat call.
func (c *evalCtx) pushConcatArg(t grammar.Term) error {
	c.p.stepWork()
	if call, ok := t.(*grammar.Call); ok && c.p.prog.projected[call] {
		v, err := c.eval(call.Args[0])
		if err != nil {
			return err
		}
		return c.pushElems("concat", []any{v})
	}
	if call, ok := t.(*grammar.Call); ok && (call.Func == "list" || call.Func == "map" && len(call.Args) == 2) {
		p := c.p
		base := len(p.estack)
		for _, a := range call.Args {
			c.p.stepWork()
			v, err := c.eval(a)
			if err != nil {
				p.estack = p.estack[:base]
				return err
			}
			p.estack = append(p.estack, v)
		}
		err := c.pushElems(call.Func, p.estack[base:])
		p.estack = p.estack[:base]
		return err
	}
	if call, ok := t.(*grammar.Call); ok && call.Func == "concat" {
		for _, a := range call.Args {
			c.p.stepWork()
			if err := c.pushConcatArg(a); err != nil {
				return err
			}
		}
		return nil
	}
	v, err := c.eval(t)
	if err != nil {
		return err
	}
	return c.pushElems("concat", []any{v})
}

// index returns element i of the list l.
func (p *parser) index(l, i any) (any, error) {
	items, err := p.valueListItems("[]", l)
	if err != nil {
		return nil, err
	}
	n, ok := i.(int)
	if !ok {
		return nil, p.actionError("index must be int, got ", typeName(i))
	}
	if n < 0 || n >= len(items) {
		return nil, p.actionError("index ", strconv.Itoa(n), " out of range for ", strconv.Itoa(len(items)), " elements")
	}
	return value.UnboxNode(items[n]), nil
}

func (p *parser) valueListItems(fn string, v any) ([]*value.Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *value.Node:
		if x == nil {
			return nil, nil
		}
		return value.D(x).Children, nil
	}
	if workControlsEnabled && p != nil && p.control != nil {
		return nil, p.actionError(fn, ": expected a list, got ", typeName(v))
	}
	return nil, fmt.Errorf("%s: expected a list, got %s", fn, typeName(v))
}

// element returns v as an element of a list that the list built-in fn makes. An int, string or
// bool travels boxed, with the range of the action's input, as the result of an action does.
func (c *evalCtx) element(fn string, v any) (*value.Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *value.Node:
		return x, nil
	case int, string, bool:
		return c.p.newNode(value.Data{Kind: value.BoxKind, Box: x, Start: int32(c.start), End: int32(c.end)}), nil
	}
	return nil, c.p.actionError(fn, ": elements must be values, got ", typeName(v))
}

func (f *closure) apply(x, y any) (any, error) {
	// The context and the parameters are freed when the call returns; a lambda created by the body
	// keeps a copy of them (keep).
	p := f.ctx.p
	p.stepWork()
	mc, ml := p.lctxs.n, p.locals.n
	ctx := p.lctxs.alloc()
	*ctx = *f.ctx
	args := [2]any{x, y}
	for i, name := range f.params {
		p.stepWork()
		l := p.locals.alloc()
		*l = local{name: name, val: args[i], next: ctx.locals}
		ctx.locals = l
	}
	v, err := ctx.eval(f.body)
	resetWork(p, &p.lctxs, mc)
	resetWork(p, &p.locals, ml)
	return v, err
}

// keep returns c, or a copy of c on the heap if it belongs to a lambda call (closure.apply),
// whose context and parameters are freed when the call returns while a lambda created in it can
// be returned from it.
func (c *evalCtx) keep() *evalCtx {
	if c == &c.p.ectx {
		return c
	}
	k := *c
	var head *local
	for l, link := c.locals, &head; l != nil; l = l.next {
		c.p.stepWork()
		*link = &local{name: l.name, val: l.val}
		link = &(*link).next
	}
	k.locals = head
	return &k
}
