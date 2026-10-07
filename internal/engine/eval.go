package engine

import (
	"errors"
	"fmt"

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
	locals     *local  // lambda arguments and Pratt's $lhs, $rhs, $op
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
	if err != nil {
		p.fail("action in %s: %v", where, err)
	}
	if v == nil {
		return nil
	}
	n, ok := v.(*Node)
	if !ok {
		p.fail("action in %s: result must be a node, got %s", where, typeName(v))
	}
	for _, c := range ctx.p.created[ctx.cbase:] {
		if c == n {
			n.Start, n.End = ctx.start, ctx.end
			break
		}
	}
	ctx.p.created = ctx.p.created[:ctx.cbase]
	return n
}

func typeName(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case *Node:
		if v == nil {
			return "nil"
		}
		return v.Type
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
			if l.name == t.Name {
				return l.val, nil
			}
		}
		if i, ok := c.scope.slots[t.Name]; ok && c.frame != nil {
			return nodeOrNil(c.frame.vals[i]), nil
		}
		return nil, fmt.Errorf("undefined capture $%s", t.Name)

	case *grammar.IndexRef:
		if t.Index == 0 {
			return c.newList(c.items), nil
		}
		if t.Index > len(c.items) {
			return nil, fmt.Errorf("$%d is out of range (%d elements)", t.Index, len(c.items))
		}
		return nodeOrNil(c.items[t.Index-1]), nil

	case *grammar.VarRef:
		v, ok := c.p.env.lookup(t.Name)
		if !ok {
			return nil, fmt.Errorf("variable %s is not defined", t.Name)
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
		for _, fi := range t.Fields {
			v, err := c.eval(fi.Value)
			if err != nil {
				p.estack = p.estack[:base]
				return nil, err
			}
			p.estack = append(p.estack, v)
		}
		p.names = p.names[:0]
		for _, fi := range t.Fields {
			p.names = append(p.names, fi.Name)
		}
		n, err := c.newStruct(t.Type, p.names, p.estack[base:])
		p.estack = p.estack[:base]
		return n, err

	case *grammar.Call:
		// The arguments are kept on the expression stack, as in vmProgram.eval: lambdas called by the
		// built-in push above them, and built-ins do not retain args.
		p := c.p
		base := len(p.estack)
		for _, a := range t.Args {
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
		return unaryOp(t.Op, x)

	case *grammar.Binary:
		return c.binary(t)

	case *grammar.Assign:
		return nil, fmt.Errorf("assignment %s = ... is only allowed as a whole predicate", t.Name)
	}
	return nil, fmt.Errorf("unsupported term %T", t)
}

func (c *evalCtx) member(x any, name string) (any, error) {
	n, ok := x.(*Node)
	if !ok || n == nil {
		return nil, fmt.Errorf("cannot access .%s of %s", name, typeName(x))
	}
	switch name {
	case "startPos":
		return n.Start, nil
	case "endPos":
		return n.End, nil
	case "children":
		return c.newList(n.Children), nil
	}
	if st, ok := c.p.prog.types[n.Type].(*grammar.StructSpec); ok && fieldOf(st, name) == nil {
		return nil, fmt.Errorf("%s has no field %s", n.Type, name)
	}
	// Unset fields and unmatched captures are nil.
	v, _ := n.Fields.Get(name)
	return v, nil
}

// newStruct creates a struct node. Its range covers the ranges of the fields whose values are
// nodes, or is the rule's range if there are none.
func (c *evalCtx) newStruct(typ string, names []string, vals []any) (*Node, error) {
	n := c.p.newNode(Node{Type: typ, Fields: c.p.fields(len(names))})
	first := true
	for i, name := range names {
		v := vals[i]
		if _, isFn := v.(function); isFn {
			return nil, fmt.Errorf("field %s: cannot store a function", name)
		}
		n.Fields.set(name, v)
		if child, ok := v.(*Node); ok && child != nil {
			if first || child.Start < n.Start {
				n.Start = child.Start
			}
			if first || child.End > n.End {
				n.End = child.End
			}
			first = false
		}
	}
	if first {
		n.Start, n.End = c.start, c.end
	}
	c.p.created = append(c.p.created, n)
	return n, nil
}

func unaryOp(op string, x any) (any, error) {
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
	return nil, fmt.Errorf("invalid operand %s for unary %s", typeName(x), op)
}

func (c *evalCtx) newList(items []*Node) *Node {
	return c.listNode(append(c.p.nodes(len(items))[:0], items...))
}

// listNode returns a List node with the child slice kids (which it takes over).
func (c *evalCtx) listNode(kids []*Node) *Node {
	n := c.p.newNode(Node{Type: TypeList, Children: kids, Start: c.start, End: c.start})
	first := true
	for _, it := range kids {
		if it == nil {
			continue
		}
		if first || it.Start < n.Start {
			n.Start = it.Start
		}
		if first || it.End > n.End {
			n.End = it.End
		}
		first = false
	}
	return n
}

func (c *evalCtx) binary(t *grammar.Binary) (any, error) {
	l, err := c.eval(t.L)
	if err != nil {
		return nil, err
	}
	// Logical operators short-circuit.
	if t.Op == "&&" || t.Op == "||" {
		lb, ok := l.(bool)
		if !ok {
			return nil, fmt.Errorf("invalid operand %s for %s", typeName(l), t.Op)
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
			return nil, fmt.Errorf("invalid operand %s for %s", typeName(r), t.Op)
		}
		return rb, nil
	}
	r, err := c.eval(t.R)
	if err != nil {
		return nil, err
	}
	return binaryOp(t.Op, l, r)
}

// binaryOp performs a binary operation other than a logical one.
func binaryOp(op string, l, r any) (any, error) {
	switch op {
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
					return nil, errors.New("division by zero")
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
	"len": 1, "text": 1, "foldl": 3, "foldr": 3, "map": 2, "list": -1, "concat": -1,
}

// builtin calls the built-in function fn with evaluated arguments.
func (c *evalCtx) builtin(fn string, args []any) (any, error) {
	switch fn {
	case "len":
		switch x := args[0].(type) {
		case string:
			return c.p.unit.textLen(x), nil
		case *Node:
			if x == nil {
				return 0, nil
			}
			if x.terminal {
				return c.p.unit.textLen(x.Text), nil
			}
			return len(x.Children), nil
		case nil:
			return 0, nil
		}
	case "text":
		switch x := args[0].(type) {
		case string:
			return x, nil
		case *Node:
			if x == nil {
				return "", nil
			}
			if x.terminal {
				return x.Text, nil
			}
			return c.p.text(x.Start, x.End), nil
		}
	case "foldl", "foldr":
		f, ok := args[2].(function)
		if !ok || f.arity() != 2 {
			return nil, fmt.Errorf("%s: third argument must be a function of two parameters", fn)
		}
		items, err := listItems(fn, args[1])
		if err != nil {
			return nil, err
		}
		acc := args[0]
		for i := range items {
			it := items[i]
			if fn == "foldr" {
				it = items[len(items)-1-i]
			}
			if acc, err = f.apply(acc, nodeOrNil(it)); err != nil {
				return nil, err
			}
		}
		return acc, nil
	case "map":
		f, ok := args[1].(function)
		if !ok || f.arity() != 1 {
			return nil, errors.New("map: second argument must be a function of one parameter")
		}
		items, err := listItems("map", args[0])
		if err != nil {
			return nil, err
		}
		// The elements are gathered on kidStack; lambdas that build lists push above them.
		base := len(c.p.kidStack)
		for _, it := range items {
			v, err := f.apply(nodeOrNil(it), nil)
			if err != nil {
				c.p.dropKids(base)
				return nil, err
			}
			n, err := asNode("map", v)
			if err != nil {
				c.p.dropKids(base)
				return nil, err
			}
			c.p.kidStack = append(c.p.kidStack, n)
		}
		return c.listNode(c.p.kids(base)), nil
	case "list":
		base := len(c.p.kidStack)
		for _, a := range args {
			n, err := asNode("list", a)
			if err != nil {
				c.p.dropKids(base)
				return nil, err
			}
			c.p.kidStack = append(c.p.kidStack, n)
		}
		return c.listNode(c.p.kids(base)), nil
	case "concat":
		base := len(c.p.kidStack)
		for _, a := range args {
			items, err := listItems("concat", a)
			if err != nil {
				c.p.dropKids(base)
				return nil, err
			}
			c.p.kidStack = append(c.p.kidStack, items...)
		}
		return c.listNode(c.p.kids(base)), nil
	default:
		return nil, fmt.Errorf("unknown function %s", fn)
	}
	return nil, fmt.Errorf("%s: invalid argument %s", fn, typeName(args[0]))
}

func listItems(fn string, v any) ([]*Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *Node:
		if x == nil {
			return nil, nil
		}
		return x.Children, nil
	}
	return nil, fmt.Errorf("%s: expected a list, got %s", fn, typeName(v))
}

func asNode(fn string, v any) (*Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *Node:
		return x, nil
	}
	return nil, fmt.Errorf("%s: list elements must be nodes, got %s", fn, typeName(v))
}

func (f *closure) apply(x, y any) (any, error) {
	// The context and the parameters are freed when the call returns; a lambda created by the body
	// keeps a copy of them (keep).
	p := f.ctx.p
	mc, ml := p.lctxs.n, p.locals.n
	ctx := p.lctxs.alloc()
	*ctx = *f.ctx
	args := [2]any{x, y}
	for i, name := range f.params {
		l := p.locals.alloc()
		*l = local{name: name, val: args[i], next: ctx.locals}
		ctx.locals = l
	}
	v, err := ctx.eval(f.body)
	p.lctxs.reset(mc)
	p.locals.reset(ml)
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
		*link = &local{name: l.name, val: l.val}
		link = &(*link).next
	}
	k.locals = head
	return &k
}

// predicate compiles a predicate [...].
func (c *compiler) predicate(e *grammar.Predicate, s *scope) matcher {
	c.noIndex = "predicates"
	defer func() { c.noIndex = "" }()
	if a, ok := e.Term.(*grammar.Assign); ok {
		c.checkTerm(a.Value, s, nil)
		return func(p *parser) (*Node, bool) {
			ctx := p.useCtx(evalCtx{p: p, scope: s, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
			v, err := ctx.eval(a.Value)
			p.created = p.created[:ctx.cbase] // discard nodes created by the predicate
			if err != nil {
				return nil, false
			}
			if _, isFn := v.(*closure); isFn {
				return nil, false
			}
			p.env = &env{name: a.Name, val: v, next: p.env}
			return nil, true
		}
	}
	c.checkTerm(e.Term, s, nil)
	return func(p *parser) (*Node, bool) {
		ctx := p.useCtx(evalCtx{p: p, scope: s, frame: p.frame, start: p.pos, end: p.pos, cbase: len(p.created)})
		v, err := ctx.eval(e.Term)
		p.created = p.created[:ctx.cbase] // discard nodes created by the predicate
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
	switch t := t.(type) {
	case *grammar.CaptureRef:
		if contains(locals, t.Name) {
			return
		}
		if _, ok := s.slots[t.Name]; !ok {
			c.errorf(t.Pos, "undefined capture $%s", t.Name)
		}
	case *grammar.IndexRef:
		if c.noIndex != "" {
			c.errorf(t.Pos, "$%d cannot be used in %s", t.Index, c.noIndex)
		}
	case *grammar.Member:
		c.checkTerm(t.X, s, locals)
	case *grammar.New:
		st, ok := c.prog.types[t.Type].(*grammar.StructSpec)
		if !ok {
			c.errorf(t.Pos, "%s is not a struct type", t.Type)
		}
		seen := map[string]bool{}
		for _, fi := range t.Fields {
			if seen[fi.Name] {
				c.errorf(fi.Pos, "duplicate field %s", fi.Name)
			}
			seen[fi.Name] = true
			if ok && fieldOf(st, fi.Name) == nil {
				c.errorf(fi.Pos, "%s has no field %s", t.Type, fi.Name)
			}
			c.checkTerm(fi.Value, s, locals)
		}
	case *grammar.Call:
		n, ok := builtins[t.Func]
		if !ok {
			c.errorf(t.Pos, "unknown function %s", t.Func)
		} else if n >= 0 && n != len(t.Args) {
			c.errorf(t.Pos, "%s takes %d arguments, got %d", t.Func, n, len(t.Args))
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
	case *grammar.Assign:
		c.errorf(t.Pos, "assignment %s = ... is only allowed as a whole predicate", t.Name)
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
