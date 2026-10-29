package engine

import (
	"fmt"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/value"
)

// closureMachine is the state of the closure backend's evaluator for one parse, beside the parser
// it runs on: the lambdas of the running evaluation, the contexts and parameters of the lambda
// calls in progress, and the field names of a struct being built. The parser holds it as its
// backend state (closureMachineOf).
type closureMachine struct {
	p        *parser
	names    []string       // field names for newStruct
	closures arena[closure] // lambdas; they live until the next evaluation (evalTerm)
	lctxs    arena[evalCtx] // contexts of the lambda calls in progress (closure.apply)
	locals   arena[local]   // parameters of the lambda calls in progress
}

// closureMachineOf returns the closure evaluator state of the parse that p runs, making it on
// first use.
func closureMachineOf(p *parser) *closureMachine {
	if cm, ok := p.backend.(*closureMachine); ok {
		return cm
	}
	cm := &closureMachine{p: p}
	p.backend = cm
	return cm
}

func (cm *closureMachine) splitChunks()   {}
func (cm *closureMachine) clearInactive() {}

func (cm *closureMachine) attach(p *parser) { cm.p = p }

// keep keeps nothing: the lambdas and their contexts belong to the parse that made them.
func (cm *closureMachine) keep(int, bool) { cm.release() }

func (cm *closureMachine) release() { *cm = closureMachine{} }

func (cm *closureMachine) retainedBytes() uint64 { return 0 }

// closure is a lambda of the closure backend: its body is evaluated as an AST.
type closure struct {
	params []string
	body   grammar.Term
	ctx    *evalCtx
	cm     *closureMachine
}

func (f *closure) arity() int { return len(f.params) }

// termEvaluator returns an evaluator for an AST value expression.
func termEvaluator(t grammar.Term) evaluator {
	return func(ctx *evalCtx) (any, error) { return evalTerm(ctx, t) }
}

// evalTerm evaluates the value expression t in a new evaluation context (parser.useCtx). Such
// evaluations never nest, and no lambda outlives one, so it reuses the lambdas of the previous
// evaluation.
func evalTerm(ctx *evalCtx, t grammar.Term) (any, error) {
	cm := closureMachineOf(ctx.p)
	resetWork(ctx.p, &cm.closures, 0)
	return cm.eval(ctx, t)
}

// convert evaluates the node type constructor t, T{x}: it converts the value of x to the named
// node type T.
func (cm *closureMachine) convert(c *evalCtx, t *grammar.New) (any, error) {
	x, err := cm.eval(c, t.Args[0])
	if err != nil {
		return nil, err
	}
	p := c.p
	ks := p.prog.NodeKinds[t.Type]
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

func (cm *closureMachine) eval(c *evalCtx, t grammar.Term) (any, error) {
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
		if i, ok := c.scope.Slots[t.Name]; ok && c.frame != nil {
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
		x, err := cm.eval(c, t.X)
		if err != nil {
			return nil, err
		}
		return c.member(x, t.Name)

	case *grammar.New:
		if len(t.Args) != 0 {
			return cm.convert(c, t)
		}
		// The values are kept on the expression stack (evaluations nested in them push above), and
		// the names in a buffer filled once no evaluation is pending; newStruct retains neither.
		p := c.p
		base := len(p.estack)
		for _, fi := range t.Fields {
			p.stepWork()
			v, err := cm.eval(c, fi.Value)
			if err != nil {
				p.estack = p.estack[:base]
				return nil, err
			}
			p.estack = append(p.estack, v)
		}
		cm.names = cm.names[:0]
		for _, fi := range t.Fields {
			p.stepWork()
			cm.names = append(cm.names, fi.Name)
		}
		n, err := c.newStruct(t.Type, cm.names, p.estack[base:])
		p.estack = p.estack[:base]
		return n, err
	case *grammar.ListConstructor:
		return cm.evalList(c, t.Items)
	case *grammar.TupleLit:
		return cm.evalTuple(c, t.Elems)

	case *grammar.Call:
		if c.p.prog.Projected[t] {
			// map($x, (e) => e.$f) of a projected repetition: $x holds the values already.
			x, err := cm.eval(c, t.Args[0])
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
				if err := cm.pushConcatArg(c, a); err != nil {
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
			v, err := cm.eval(c, a)
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
		f := cm.closures.alloc()
		*f = closure{params: t.Params, body: t.Body, ctx: c.keep(), cm: cm}
		return f, nil

	case *grammar.Unary:
		x, err := cm.eval(c, t.X)
		if err != nil {
			return nil, err
		}
		return c.p.valueUnary(t.Op, x)

	case *grammar.Cast:
		x, err := cm.eval(c, t.X)
		if err != nil {
			return nil, err
		}
		if ft := c.p.prog.CastType(t); ft != nil && !c.p.fieldConforms(ft, x) {
			line, col := c.p.prog.CastPos(t)
			return nil, c.p.castError(x, ft, c.start, line, col)
		}
		return x, nil

	case *grammar.Binary:
		return cm.binary(c, t)

	case *grammar.Bind:
		return nil, c.p.actionError("binding ", t.Name, " := ... is only allowed as a whole predicate")
	}
	return nil, fmt.Errorf("unsupported term %T", t)
}

// evalList shares the existing list builder without allocating a synthetic AST.
func (cm *closureMachine) evalList(c *evalCtx, items []grammar.Term) (any, error) {
	p := c.p
	base := len(p.estack)
	for _, item := range items {
		val, err := cm.eval(c, item)
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
func (cm *closureMachine) evalTuple(c *evalCtx, elems []grammar.Term) (any, error) {
	p := c.p
	base := len(p.estack)
	for _, e := range elems {
		val, err := cm.eval(c, e)
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

func (cm *closureMachine) binary(c *evalCtx, t *grammar.Binary) (any, error) {
	if t.Op == "==" || t.Op == "!=" {
		// Comparing two texts (such as an end tag with its start tag) compares the strings
		// directly: storing a string in an interface would allocate.
		if lx, ok := compile.TextArg(t.L); ok {
			if rx, ok := compile.TextArg(t.R); ok {
				ls, err := cm.textOf(c, lx)
				if err != nil {
					return nil, err
				}
				rs, err := cm.textOf(c, rx)
				if err != nil {
					return nil, err
				}
				return c.p.valueEqual(ls, rs) == (t.Op == "=="), nil
			}
		}
	}
	l, err := cm.eval(c, t.L)
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
		r, err := cm.eval(c, t.R)
		if err != nil {
			return nil, err
		}
		rb, ok := r.(bool)
		if !ok {
			return nil, c.p.actionError("invalid operand ", typeName(r), " for ", t.Op)
		}
		return rb, nil
	}
	r, err := cm.eval(c, t.R)
	if err != nil {
		return nil, err
	}
	return c.p.valueBinary(t.Op, l, r)
}

// textOf evaluates x and returns text(x) as a string.
func (cm *closureMachine) textOf(c *evalCtx, x grammar.Term) (string, error) {
	v, err := cm.eval(c, x)
	if err != nil {
		return "", err
	}
	return c.textString(v)
}

// pushConcatArg pushes onto kidStack the elements of t, an argument of a fusable concat call.
func (cm *closureMachine) pushConcatArg(c *evalCtx, t grammar.Term) error {
	c.p.stepWork()
	if call, ok := t.(*grammar.Call); ok && c.p.prog.Projected[call] {
		v, err := cm.eval(c, call.Args[0])
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
			v, err := cm.eval(c, a)
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
			if err := cm.pushConcatArg(c, a); err != nil {
				return err
			}
		}
		return nil
	}
	v, err := cm.eval(c, t)
	if err != nil {
		return err
	}
	return c.pushElems("concat", []any{v})
}

func (f *closure) apply(x, y any) (any, error) {
	// The context and the parameters are freed when the call returns; a lambda created by the body
	// keeps a copy of them (keep).
	cm := f.cm
	p := f.ctx.p
	p.stepWork()
	mc, ml := cm.lctxs.n, cm.locals.n
	ctx := cm.lctxs.alloc()
	*ctx = *f.ctx
	args := [2]any{x, y}
	for i, name := range f.params {
		p.stepWork()
		l := cm.locals.alloc()
		*l = local{name: name, val: args[i], next: ctx.locals}
		ctx.locals = l
	}
	v, err := cm.eval(ctx, f.body)
	resetWork(p, &cm.lctxs, mc)
	resetWork(p, &cm.locals, ml)
	return v, err
}
