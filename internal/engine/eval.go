package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ornew/pego/grammar"
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
	items      []*Node // elements referenced by $n
	start, end int     // range matched by the rule
	locals     *local  // lambda arguments and OperatorsRule's $lhs, $rhs, $op
	cbase      int     // start, in p.created, of the nodes created by this evaluation
}

// runAction evaluates the rule's action.
func (p *parser) runAction(r *rule, f *frame, items []*Node, start, end int) *Node {
	ctx := p.useCtx(evalCtx{p: p, scope: r.scope, frame: f, items: items, start: start, end: end, cbase: len(p.created)})
	return p.actionResult(ctx, r.act, r.name)
}

// termEvaluator returns an evaluator for an AST value expression.
func termEvaluator(t grammar.Term) evaluator {
	return func(ctx *evalCtx) (any, error) { return ctx.eval(t) }
}

func (p *parser) actionResult(ctx *evalCtx, act evaluator, where string) *Node {
	v, err := act(ctx)
	n, ok := v.(*Node)
	if err == nil && n != nil {
		for _, c := range p.created[ctx.cbase:] {
			p.stepWork()
			if c == n {
				n.start, n.end = int32(ctx.start), int32(ctx.end)
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
		return p.newNode(Node{kind: kindBox, box: v, start: int32(ctx.start), end: int32(ctx.end)})
	}
	if n != nil {
		// The value of an action is final: a node it returns from the rule body (=> $1) is not
		// labeled by the rules that receive it, which might share it through the memo.
		n.fresh = false
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
	case *Node:
		if v == nil {
			return "nil"
		}
		if v.kind == kindBox {
			return typeName(v.box)
		}
		return valueTypeName(v.shape(), v.Type(), len(v.children))
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
				return unboxValue(l.val), nil
			}
		}
		if i, ok := c.scope.slots[t.Name]; ok && c.frame != nil {
			return unboxNode(c.frame.vals[i]), nil
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
		// The values are kept on the expression stack (evaluations nested in them push above), and
		// the names in a buffer filled once no evaluation is pending; newStruct retains neither.
		p := c.p
		base := len(p.estack)
		checks := p.prog.constructorChecks(t)
		for i, fi := range t.Fields {
			p.stepWork()
			v, err := c.eval(fi.Value)
			if err != nil {
				p.estack = p.estack[:base]
				return nil, err
			}
			if checks != nil && checks[i] != nil && !p.fieldConforms(checks[i], v) {
				p.estack = p.estack[:base]
				name, _ := p.prog.structType(t.Type)
				return nil, p.fieldTypeError(v, checks[i], fi.Name, name)
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
			return nil, c.p.castError(x, ft)
		}
		return x, nil

	case *grammar.Binary:
		return c.binary(t)

	case *grammar.Assign:
		return nil, c.p.actionError("assignment ", t.Name, " = ... is only allowed as a whole predicate")
	}
	return nil, fmt.Errorf("unsupported term %T", t)
}

// evalList shares the existing list builder without allocating a synthetic AST.
func (c *evalCtx) evalList(items []grammar.Term) (any, error) {
	p := c.p
	base := len(p.estack)
	for _, item := range items {
		value, err := c.eval(item)
		if err != nil {
			p.estack = p.estack[:base]
			return nil, err
		}
		p.estack = append(p.estack, value)
	}
	value, err := c.builtin("list", p.estack[base:])
	p.estack = p.estack[:base]
	return value, err
}

// nodeMembers are the members of a node, and the removed ones that the checker reports: they are
// not captures, which are read with x.$name.
var nodeMembers = map[string]bool{"Start": true, "End": true, "Value": true, "startPos": true, "endPos": true, "children": true}

func (c *evalCtx) member(x any, name string) (any, error) {
	o, err := c.memberOperand(x, name)
	return o.value(), err
}

// memberOperand reads x.name, keeping the text of a text struct unboxed.
func (c *evalCtx) memberOperand(x any, name string) (operand, error) {
	n, ok := x.(*Node)
	if !ok || n == nil {
		return operand{}, c.p.actionError("cannot access .", name, " of ", typeName(x))
	}
	if n.textField && name == "Text" && (!workControlsEnabled || c.p.control == nil) {
		// The text of a text struct, its one field, read without looking the struct up. Work
		// controls charge the lookup below.
		return operand{str: n.text, isStr: true}, nil
	}
	if n.isNode() {
		switch name {
		case "Start":
			return operand{v: int(n.start)}, nil
		case "End":
			return operand{v: int(n.end)}, nil
		case "Value":
			return operand{v: c.p.nodeValue(n)}, nil
		}
	}
	if capture, ok := strings.CutPrefix(name, "$"); ok {
		// x.$y reads a capture, which struct values do not have; an unmatched capture is nil.
		if _, ok := c.p.prog.types[n.Type()].(*grammar.StructSpec); ok {
			return operand{}, c.p.actionError(n.Type(), " has no capture ", capture)
		}
		v, _ := c.p.valueField(n.fields, capture)
		return operand{v: unboxValue(v)}, nil
	}
	if st, ok := c.p.prog.types[n.Type()].(*grammar.StructSpec); ok && !c.p.hasValueField(st, name) {
		return operand{}, c.p.actionError(n.Type(), " has no field ", name)
	}
	// Unset fields and unmatched captures are nil.
	if n.textField {
		if c.p.textFieldName(name) {
			return operand{str: n.text, isStr: true}, nil
		}
		return operand{}, nil
	}
	v, _ := c.p.valueField(n.fields, name)
	return operand{v: v}, nil
}

// newStruct creates a struct node. Its range covers the ranges of the fields whose values are
// nodes, or is the rule's range if there are none.
func (c *evalCtx) newStruct(typ string, names []string, vals []any) (*Node, error) {
	n := c.p.newNode(Node{kind: c.p.prog.typeKind(typ), fields: c.p.fields(len(names))})
	first := true
	for i, name := range names {
		c.p.stepWork()
		v := vals[i]
		if _, isFn := v.(function); isFn {
			return nil, c.p.actionError("field ", name, ": cannot store a function")
		}
		c.p.setValueField(&n.fields, name, v)
		if child, ok := v.(*Node); ok && child != nil {
			if first || child.start < n.start {
				n.start = child.start
			}
			if first || child.end > n.end {
				n.end = child.end
			}
			first = false
		}
	}
	if first {
		n.start, n.end = int32(c.start), int32(c.end)
	}
	c.p.created = append(c.p.created, n)
	return n, nil
}

func unaryOp(op string, x any) (any, error) {
	return (*parser)(nil).valueUnary(op, x)
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

func (c *evalCtx) newList(items []*Node) *Node {
	kids := c.p.nodes(len(items))
	copyWork(c.p, kids, items)
	return c.listNode(kids)
}

// listNode returns a list value with the child slice kids (which it takes over). Its range,
// which the value model does not show, covers the elements that are nodes.
func (c *evalCtx) listNode(kids []*Node) *Node {
	n := c.p.newNode(Node{kind: kindListValue, children: kids, start: int32(c.start), end: int32(c.start)})
	first := true
	for _, it := range kids {
		c.p.stepWork()
		if it == nil {
			continue
		}
		if first || it.start < n.start {
			n.start = it.start
		}
		if first || it.end > n.end {
			n.end = it.end
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
	if comparison(t.Op) && (fieldRead(t.L) || fieldRead(t.R)) && !c.p.prog.boxedComparisons {
		return c.compare(t)
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

// operand is an operand of a comparison: the text of a text struct read without boxing it (str,
// with isStr), or any other value (v).
type operand struct {
	v     any
	str   string
	isStr bool
}

// operand evaluates t, keeping the Text field of a text struct unboxed.
func (c *evalCtx) operand(t grammar.Term) (operand, error) {
	m, ok := t.(*grammar.Member)
	if !ok || !fieldRead(t) {
		v, err := c.eval(t)
		return operand{v: v}, err
	}
	c.p.stepWork() // The step that eval charges for the member read.
	x, err := c.eval(m.X)
	if err != nil {
		return operand{}, err
	}
	return c.memberOperand(x, m.Name)
}

// compare applies the comparison of t as binary does, comparing two strings directly: storing
// the text of a text struct in an interface would allocate.
func (c *evalCtx) compare(t *grammar.Binary) (any, error) {
	l, err := c.operand(t.L)
	if err != nil {
		return nil, err
	}
	r, err := c.operand(t.R)
	if err != nil {
		return nil, err
	}
	return c.p.compareOperands(t.Op, l, r)
}

// compareOperands applies the comparison operator op as valueBinary does.
func (p *parser) compareOperands(op string, l, r operand) (any, error) {
	if !l.isStr {
		l.str, l.isStr = l.v.(string)
	}
	if !r.isStr {
		r.str, r.isStr = r.v.(string)
	}
	if l.isStr && r.isStr {
		if v, ok := p.compareStrings(op, l.str, r.str); ok {
			return v, nil
		}
	}
	return p.valueBinary(op, l.value(), r.value())
}

// textOperand is the node of a text struct on the value stack of the action VM, standing for its
// text as an operand of a comparison. Only EMEMBER with B 1 pushes it and only the EBIN with B 1
// that the compiler emits for the comparison pops it, so it never escapes into a value.
type textOperand Node

// vmOperand is the operand of a comparison for v, a value on the stack of the action VM.
func vmOperand(v any) operand {
	if t, ok := v.(*textOperand); ok {
		return operand{str: t.text, isStr: true}
	}
	return operand{v: v}
}

func (o operand) value() any {
	if o.isStr && o.v == nil {
		return o.str
	}
	return o.v
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
	case *Node:
		if a == nil {
			return b == nil
		}
		bn, ok := b.(*Node)
		return ok && a == bn
	case nil:
		if bn, ok := b.(*Node); ok {
			return bn == nil
		}
		return b == nil
	case int, string, bool:
		return a == b
	}
	return false
}

// builtins maps built-in functions to their number of arguments (-1 for variadic).
var builtins = map[string]int{
	"len": 1, "text": 1, "foldl": 3, "foldr": 3, "map": 2, "list": -1, "concat": -1, "filter": 2,
}

// builtin calls the built-in function fn with evaluated arguments.
func (c *evalCtx) builtin(fn string, args []any) (any, error) {
	switch fn {
	case "len":
		switch x := args[0].(type) {
		case string:
			return c.p.valueTextLen(x), nil
		case *Node:
			if x == nil {
				return 0, nil
			}
			if x.terminal {
				return c.p.valueTextLen(x.text), nil
			}
			return len(x.children), nil
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
			if acc, err = f.apply(acc, unboxNode(it)); err != nil {
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

// textArg returns the argument of t if t is a call text(x).
func textArg(t grammar.Term) (grammar.Term, bool) {
	if call, ok := t.(*grammar.Call); ok && call.Func == "text" && len(call.Args) == 1 {
		return call.Args[0], true
	}
	return nil, false
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
	case *Node:
		if v == nil {
			return "", nil
		}
		if v.terminal {
			return v.text, nil
		}
		return c.p.text(int(v.start), int(v.end)), nil
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
			v, err := f.apply(unboxNode(it), nil)
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

// fusable reports whether every argument of the concat call t is itself a call of list, map or a
// fusable concat. Their elements can then be pushed directly, without building the intermediate
// lists (concat(list($first), map($rest, ...)) is the common way to build a list), and errors
// still arise in the same order as when the arguments are evaluated first: each call checks its
// elements as it makes them, and concat has nothing left to check. With any other argument,
// concat would check it only after all arguments are evaluated.
func fusable(t *grammar.Call) bool {
	for _, a := range t.Args {
		call, ok := a.(*grammar.Call)
		switch {
		case ok && call.Func == "list", ok && call.Func == "map" && len(call.Args) == 2:
		case ok && call.Func == "concat" && fusable(call):
		default:
			return false
		}
	}
	return true
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
		return nil, p.actionError("index ", strconv.Itoa(n), " out of range for a list of ", strconv.Itoa(len(items)), " elements")
	}
	return unboxNode(items[n]), nil
}

func listItems(fn string, v any) ([]*Node, error) {
	return (*parser)(nil).valueListItems(fn, v)
}

func (p *parser) valueListItems(fn string, v any) ([]*Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *Node:
		if x == nil {
			return nil, nil
		}
		return x.children, nil
	}
	if workControlsEnabled && p != nil && p.control != nil {
		return nil, p.actionError(fn, ": expected a list, got ", typeName(v))
	}
	return nil, fmt.Errorf("%s: expected a list, got %s", fn, typeName(v))
}

// element returns v as an element of a list that the list built-in fn makes. An int, string or
// bool travels boxed, with the range of the action's input, as the result of an action does.
func (c *evalCtx) element(fn string, v any) (*Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *Node:
		return x, nil
	case int, string, bool:
		return c.p.newNode(Node{kind: kindBox, box: x, start: int32(c.start), end: int32(c.end)}), nil
	}
	return nil, c.p.actionError(fn, ": list elements must be values, got ", typeName(v))
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

// predicate compiles a Predicate Action { ... }.
func (c *compiler) predicate(e *grammar.Predicate, s *scope) matcher {
	if a, ok := e.Term.(*grammar.Assign); ok {
		c.checkTerm(a.Value, s, nil)
		return func(p *parser) (*Node, bool) {
			ctx := p.useCtx(evalCtx{p: p, scope: s, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
			v, err := ctx.eval(a.Value)
			p.dropCreated(ctx.cbase)
			if err != nil {
				return nil, false
			}
			if _, isFn := v.(*closure); isFn {
				return nil, false
			}
			p.bind(a.Name, v)
			return nil, true
		}
	}
	c.checkTerm(e.Term, s, nil)
	return func(p *parser) (*Node, bool) {
		ctx := p.useCtx(evalCtx{p: p, scope: s, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
		v, err := ctx.eval(e.Term)
		p.dropCreated(ctx.cbase)
		if err != nil {
			return nil, false
		}
		if b, ok := v.(bool); ok && !b {
			return nil, false
		}
		return nil, true
	}
}

// checkTerm resolves the names in a value expression and reports statically detectable errors.
func (c *compiler) checkTerm(t grammar.Term, s *scope, locals []string) {
	c.control.poll()
	switch t := t.(type) {
	case *grammar.CaptureRef:
		if contains(locals, t.Name) {
			return
		}
		if _, ok := s.slots[t.Name]; !ok {
			for o := s.outer; o != nil; o = o.outer {
				if _, ok := o.slots[t.Name]; ok {
					c.errorf(t.Diagnostic(), "capture $%s is not visible in a repetition element, which sees only its own captures; define a variable from it before the repetition", t.Name)
					return
				}
			}
			c.errorf(t.Diagnostic(), "undefined capture $%s", t.Name)
		}
	case *grammar.IndexRef:
		c.errorf(t.Diagnostic(), "positional capture $%d is removed; name the element with a capture", t.Index)
	case *grammar.Member:
		c.checkTerm(t.X, s, locals)
	case *grammar.New:
		_, st := c.prog.structType(t.Type)
		if st == nil {
			c.errorf(t.Diagnostic(), "%s is not a struct type", t.Type)
		}
		seen := map[string]bool{}
		for _, fi := range t.Fields {
			if seen[fi.Name] {
				c.errorf(fi.Diagnostic(), "duplicate field %s", fi.Name)
			}
			seen[fi.Name] = true
			if st != nil && fieldOf(st, fi.Name) == nil {
				c.errorf(fi.Diagnostic(), "%s has no field %s", t.Type, fi.Name)
			}
			c.checkTerm(fi.Value, s, locals)
		}
	case *grammar.ListConstructor:
		k := &typeResolver{control: c.control, prog: c.prog, errs: &c.errs, report: true}
		reported := len(c.errs)
		elem := k.resolve(t.Elem)
		if len(c.errs) > reported || k.sawUnsupported {
			// The element type is in error already.
		} else if elem == tyNever {
			c.errorf(t.Diagnostic(), "the list element has type _|_, which has no values")
		} else if !concreteListElement(elem) {
			c.errorf(t.Diagnostic(), "list constructors require a concrete element type, got %s", elem)
		}
		for _, item := range t.Items {
			c.checkTerm(item, s, locals)
		}
	case *grammar.Call:
		n, ok := builtins[t.Func]
		if !ok {
			c.errorf(t.Diagnostic(), "unknown function %s", t.Func)
		} else if n >= 0 && n != len(t.Args) {
			c.errorf(t.Diagnostic(), "%s takes %d arguments, got %d", t.Func, n, len(t.Args))
		}
		for _, a := range t.Args {
			c.checkTerm(a, s, locals)
		}
	case *grammar.Lambda:
		c.checkTerm(t.Body, s, append(append([]string(nil), locals...), t.Params...))
	case *grammar.Binary:
		c.checkTerm(t.L, s, locals)
		c.checkTerm(t.R, s, locals)
	case *grammar.Unary:
		c.checkTerm(t.X, s, locals)
	case *grammar.Cast:
		k := &typeResolver{control: c.control, prog: c.prog, errs: &c.errs, report: true}
		k.resolveLocation(t.Type, t.Diagnostic(), "the cast")
		c.checkTerm(t.X, s, locals)
	case *grammar.Assign:
		c.errorf(t.Diagnostic(), "assignment %s = ... is only allowed as a whole predicate", t.Name)
	}
}

func fieldOf(st *grammar.StructSpec, name string) *grammar.Field {
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}
