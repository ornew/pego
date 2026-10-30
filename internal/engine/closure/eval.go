package closure

import (
	"fmt"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

// Machine is the state of the closure backend's evaluator for one parse, beside the parser
// it runs on: the lambdas of the running evaluation, the contexts and parameters of the lambda
// calls in progress, and the field names of a struct being built. The parser holds it as its
// backend state; evaluation installs it on first use.
type Machine struct {
	// P is the parser the machine is attached to; nil once released.
	P        *core.Parser
	names    []string                 // field names for NewStruct
	closures core.Arena[closure]      // lambdas; they live until the next evaluation (evalTerm)
	lctxs    core.Arena[core.EvalCtx] // contexts of the lambda calls in progress (closure.Apply)
	locals   core.Arena[core.Local]   // parameters of the lambda calls in progress
}

// MachineOf returns the closure evaluator state of the parse that p runs, making it on
// first use.
func MachineOf(p *core.Parser) *Machine {
	if cm, ok := p.Backend.(*Machine); ok {
		return cm
	}
	cm := &Machine{P: p}
	p.Backend = cm
	return cm
}

func (cm *Machine) SplitChunks()   {}
func (cm *Machine) ClearInactive() {}

func (cm *Machine) Attach(p *core.Parser) { cm.P = p }

// Keep keeps nothing: the lambdas and their contexts belong to the parse that made them.
func (cm *Machine) Keep(int, bool) { cm.Release() }

func (cm *Machine) Release() { *cm = Machine{} }

func (cm *Machine) RetainedBytes() uint64 { return 0 }

// closure is a lambda of the closure backend: its body is evaluated as an AST.
type closure struct {
	params []string
	body   grammar.Term
	ctx    *core.EvalCtx
	cm     *Machine
}

func (f *closure) Arity() int { return len(f.params) }

// The closure backend implements the interfaces of core for lambdas and backend state.
var (
	_ core.Function     = (*closure)(nil)
	_ core.BackendState = (*Machine)(nil)
)

// termEvaluator returns an evaluator for an AST value expression.
func termEvaluator(t grammar.Term) core.Evaluator {
	return func(ctx *core.EvalCtx) (any, error) { return evalTerm(ctx, t) }
}

// evalTerm evaluates the value expression t in a new evaluation context (Parser.UseCtx). Such
// evaluations never nest, and no lambda outlives one, so it reuses the lambdas of the previous
// evaluation.
func evalTerm(ctx *core.EvalCtx, t grammar.Term) (any, error) {
	cm := MachineOf(ctx.P)
	core.ResetWork(ctx.P, &cm.closures, 0)
	return cm.Eval(ctx, t)
}

// convert evaluates the node type constructor t, T{x}: it converts the value of x to the named
// node type T.
func (cm *Machine) convert(c *core.EvalCtx, t *grammar.New) (any, error) {
	x, err := cm.Eval(c, t.Args[0])
	if err != nil {
		return nil, err
	}
	p := c.P
	ks := p.Prog.NodeKinds[t.Type]
	if ks == nil {
		return nil, p.ConvertError(x, t.Type)
	}
	name := ks[0].Type // the type's own name, also for an alias of it
	n, ok := ks.Convert(x)
	if !ok {
		return nil, p.ConvertError(x, name)
	}
	return n, nil
}

func (cm *Machine) Eval(c *core.EvalCtx, t grammar.Term) (any, error) {
	c.P.Work()
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
		for l := c.Locals; l != nil; l = l.Next {
			c.P.Work()
			if c.P.ValueEqual(l.Name, t.Name) {
				return value.UnboxValue(l.Val), nil
			}
		}
		if i, ok := c.Scope.Slots[t.Name]; ok && c.Frame != nil {
			return value.UnboxNode(c.Frame.Vals[i]), nil
		}
		return nil, c.P.ActionError("undefined capture $", t.Name)

	case *grammar.IndexRef:
		return nil, c.P.ActionError("positional captures are removed")

	case *grammar.VarRef:
		v, ok := c.P.LookupValue(t.Name)
		if !ok {
			return nil, c.P.ActionError("variable ", t.Name, " is not defined")
		}
		return v, nil

	case *grammar.Member:
		x, err := cm.Eval(c, t.X)
		if err != nil {
			return nil, err
		}
		return c.Member(x, t.Name)

	case *grammar.New:
		if len(t.Args) != 0 {
			return cm.convert(c, t)
		}
		// The values are kept on the expression stack (evaluations nested in them push above), and
		// the names in a buffer filled once no evaluation is pending; NewStruct retains neither.
		p := c.P
		base := len(p.Estack)
		for _, fi := range t.Fields {
			p.Work()
			v, err := cm.Eval(c, fi.Value)
			if err != nil {
				p.Estack = p.Estack[:base]
				return nil, err
			}
			p.Estack = append(p.Estack, v)
		}
		cm.names = cm.names[:0]
		for _, fi := range t.Fields {
			p.Work()
			cm.names = append(cm.names, fi.Name)
		}
		n, err := c.NewStruct(t.Type, cm.names, p.Estack[base:])
		p.Estack = p.Estack[:base]
		return n, err
	case *grammar.ListConstructor:
		return cm.evalList(c, t.Items)
	case *grammar.TupleLit:
		return cm.evalTuple(c, t.Elems)

	case *grammar.Call:
		if c.P.Prog.Projected[t] {
			// map($x, (e) => e.$f) of a projected repetition: $x holds the values already.
			x, err := cm.Eval(c, t.Args[0])
			if err != nil {
				return nil, err
			}
			items, err := c.P.ValueListItems("map", x)
			if err != nil {
				return nil, err
			}
			return c.NewList(items), nil
		}
		if t.Func == "concat" && c.P.Fusable(t) {
			base := len(c.P.KidStack)
			for _, a := range t.Args {
				c.P.Work()
				if err := cm.pushConcatArg(c, a); err != nil {
					c.P.DropKids(base)
					return nil, err
				}
			}
			return c.ListNode(c.P.Kids(base)), nil
		}
		// The arguments are kept on the expression stack, as in vm.Program.eval: lambdas called by the
		// built-in push above them, and built-ins do not retain args.
		p := c.P
		base := len(p.Estack)
		for _, a := range t.Args {
			c.P.Work()
			v, err := cm.Eval(c, a)
			if err != nil {
				p.Estack = p.Estack[:base]
				return nil, err
			}
			p.Estack = append(p.Estack, v)
		}
		v, err := c.Builtin(t.Func, p.Estack[base:])
		p.Estack = p.Estack[:base]
		return v, err

	case *grammar.Lambda:
		f := cm.closures.Alloc()
		*f = closure{params: t.Params, body: t.Body, ctx: c.Keep(), cm: cm}
		return f, nil

	case *grammar.Unary:
		x, err := cm.Eval(c, t.X)
		if err != nil {
			return nil, err
		}
		return c.P.ValueUnary(t.Op, x)

	case *grammar.Cast:
		x, err := cm.Eval(c, t.X)
		if err != nil {
			return nil, err
		}
		if ft := c.P.Prog.CastType(t); ft != nil && !c.P.FieldConforms(ft, x) {
			line, col := c.P.Prog.CastPos(t)
			return nil, c.P.CastError(x, ft, c.Start, line, col)
		}
		return x, nil

	case *grammar.Binary:
		return cm.binary(c, t)

	case *grammar.Bind:
		return nil, c.P.ActionError("binding ", t.Name, " := ... is only allowed as a whole predicate")
	}
	return nil, fmt.Errorf("unsupported term %T", t)
}

// evalList shares the existing list builder without allocating a synthetic AST.
func (cm *Machine) evalList(c *core.EvalCtx, items []grammar.Term) (any, error) {
	p := c.P
	base := len(p.Estack)
	for _, item := range items {
		val, err := cm.Eval(c, item)
		if err != nil {
			p.Estack = p.Estack[:base]
			return nil, err
		}
		p.Estack = append(p.Estack, val)
	}
	val, err := c.Builtin("list", p.Estack[base:])
	p.Estack = p.Estack[:base]
	return val, err
}

// evalTuple evaluates the elements of a tuple literal, left to right, and returns the tuple value.
func (cm *Machine) evalTuple(c *core.EvalCtx, elems []grammar.Term) (any, error) {
	p := c.P
	base := len(p.Estack)
	for _, e := range elems {
		val, err := cm.Eval(c, e)
		if err != nil {
			p.Estack = p.Estack[:base]
			return nil, err
		}
		p.Estack = append(p.Estack, val)
	}
	kb := len(p.KidStack)
	for _, v := range p.Estack[base:] {
		p.Work()
		n, err := c.Element("tuple", v)
		if err != nil {
			p.DropKids(kb)
			p.Estack = p.Estack[:base]
			return nil, err
		}
		p.KidStack = append(p.KidStack, n)
	}
	p.Estack = p.Estack[:base]
	return c.TupleNode(p.Kids(kb)), nil
}

func (cm *Machine) binary(c *core.EvalCtx, t *grammar.Binary) (any, error) {
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
				return c.P.ValueEqual(ls, rs) == (t.Op == "=="), nil
			}
		}
	}
	l, err := cm.Eval(c, t.L)
	if err != nil {
		return nil, err
	}
	// Logical operators short-circuit.
	if t.Op == "&&" || t.Op == "||" {
		lb, ok := l.(bool)
		if !ok {
			return nil, c.P.ActionError("invalid operand ", core.TypeName(l), " for ", t.Op)
		}
		if lb == (t.Op == "||") {
			return lb, nil
		}
		r, err := cm.Eval(c, t.R)
		if err != nil {
			return nil, err
		}
		rb, ok := r.(bool)
		if !ok {
			return nil, c.P.ActionError("invalid operand ", core.TypeName(r), " for ", t.Op)
		}
		return rb, nil
	}
	r, err := cm.Eval(c, t.R)
	if err != nil {
		return nil, err
	}
	return c.P.ValueBinary(t.Op, l, r)
}

// textOf evaluates x and returns text(x) as a string.
func (cm *Machine) textOf(c *core.EvalCtx, x grammar.Term) (string, error) {
	v, err := cm.Eval(c, x)
	if err != nil {
		return "", err
	}
	return c.TextString(v)
}

// pushConcatArg pushes onto KidStack the elements of t, an argument of a fusable concat call.
func (cm *Machine) pushConcatArg(c *core.EvalCtx, t grammar.Term) error {
	c.P.Work()
	if call, ok := t.(*grammar.Call); ok && c.P.Prog.Projected[call] {
		v, err := cm.Eval(c, call.Args[0])
		if err != nil {
			return err
		}
		return c.PushElems("concat", []any{v})
	}
	if call, ok := t.(*grammar.Call); ok && (call.Func == "list" || call.Func == "map" && len(call.Args) == 2) {
		p := c.P
		base := len(p.Estack)
		for _, a := range call.Args {
			c.P.Work()
			v, err := cm.Eval(c, a)
			if err != nil {
				p.Estack = p.Estack[:base]
				return err
			}
			p.Estack = append(p.Estack, v)
		}
		err := c.PushElems(call.Func, p.Estack[base:])
		p.Estack = p.Estack[:base]
		return err
	}
	if call, ok := t.(*grammar.Call); ok && call.Func == "concat" {
		for _, a := range call.Args {
			c.P.Work()
			if err := cm.pushConcatArg(c, a); err != nil {
				return err
			}
		}
		return nil
	}
	v, err := cm.Eval(c, t)
	if err != nil {
		return err
	}
	return c.PushElems("concat", []any{v})
}

func (f *closure) Apply(x, y any) (any, error) {
	// The context and the parameters are freed when the call returns; a lambda created by the body
	// keeps a copy of them (keep).
	cm := f.cm
	p := f.ctx.P
	p.Work()
	mc, ml := cm.lctxs.N, cm.locals.N
	ctx := cm.lctxs.Alloc()
	*ctx = *f.ctx
	args := [2]any{x, y}
	for i, name := range f.params {
		p.Work()
		l := cm.locals.Alloc()
		*l = core.Local{Name: name, Val: args[i], Next: ctx.Locals}
		ctx.Locals = l
	}
	v, err := cm.Eval(ctx, f.body)
	core.ResetWork(p, &cm.lctxs, mc)
	core.ResetWork(p, &cm.locals, ml)
	return v, err
}
