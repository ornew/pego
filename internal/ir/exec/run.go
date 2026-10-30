package exec

import "fmt"

// Machine implements the operations of a module: a reference model, or a target's implementation
// bound for testing. Call runs the operation o with args and returns its results. Values are bool
// for KBool, int64 for KInt, int32 for KI32, uint32 for KU32, uint64 for KU64, and any comparable
// value for a handle, so that arithmetic wraps at the width of its type. An operation marked NoReturn that ends the parse panics with an Interrupt.
type Machine interface {
	Call(o *Op, args []any) []any
}

// Interrupt is the panic value of an operation that ends the parse instead of returning.
type Interrupt struct{ Reason string }

func (i Interrupt) Error() string { return "interrupted: " + i.Reason }

// AssertionError reports an Assert that did not hold.
type AssertionError struct {
	Func, Msg string
}

func (e *AssertionError) Error() string {
	return fmt.Sprintf("func %s: assertion failed: %s", e.Func, e.Msg)
}

// Run runs the function f of the verified module m on the machine mach with args, and returns its
// results. It returns an Interrupt as the error when an operation ended the parse, and an
// *AssertionError when an Assert did not hold. Feature constants are true for the features f is
// specialized for.
func Run(m *Module, f *Func, mach Machine, args ...any) (res []any, err error) {
	defer func() {
		if x := recover(); x != nil {
			switch x := x.(type) {
			case Interrupt:
				err = x
			case *AssertionError:
				err = x
			default:
				panic(x)
			}
		}
	}()
	r := &runner{m: m, mach: mach, regs: map[string]any{}}
	for _, reg := range m.Registers {
		r.regs[reg.Name] = zero(reg.Type)
	}
	return r.call(f, args), nil
}

func zero(t Type) any {
	switch t.Kind {
	case KBool:
		return false
	case KI32:
		return int32(0)
	case KU32:
		return uint32(0)
	case KU64:
		return uint64(0)
	case KHandle:
		return nil
	}
	return int64(0)
}

type runner struct {
	m    *Module
	mach Machine
	regs map[string]any
}

// frame is the locals of one function call.
type frame struct {
	f      *Func
	locals map[string]any
}

// signal is how a statement ends: normally, or by an exit, break, continue or return.
type signal struct {
	kind   int // sigNone, sigExit, sigBreak, sigContinue, sigReturn
	label  string
	values []any
}

const (
	sigNone = iota
	sigExit
	sigBreak
	sigContinue
	sigReturn
)

func (r *runner) call(f *Func, args []any) []any {
	if len(args) != len(f.Params) {
		panic(fmt.Sprintf("exec: %s called with %d arguments, want %d", f.Name, len(args), len(f.Params)))
	}
	fr := &frame{f: f, locals: map[string]any{}}
	for i, p := range f.Params {
		fr.locals[p.Name] = args[i]
	}
	s := r.block(fr, f.Body)
	if s.kind == sigReturn {
		return s.values
	}
	return nil
}

func (r *runner) block(fr *frame, b *Block) signal {
	// Locals are visible to the end of their block; the verifier rejects shadowing, so removing
	// them on exit is enough to scope them.
	var declared []string
	defer func() {
		for _, n := range declared {
			delete(fr.locals, n)
		}
	}()
	for _, st := range b.Stmts {
		s := r.stmt(fr, st, &declared)
		if s.kind == sigExit && s.label == b.Label && b.Label != "" {
			return signal{}
		}
		if s.kind != sigNone {
			return s
		}
	}
	return signal{}
}

func (r *runner) stmt(fr *frame, st Stmt, declared *[]string) signal {
	switch st := st.(type) {
	case *Block:
		return r.block(fr, st)
	case *Loop:
		for {
			s := r.block(fr, st.Body)
			switch {
			case s.kind == sigNone, s.kind == sigContinue && s.label == st.Label:
				continue
			case (s.kind == sigBreak || s.kind == sigExit) && s.label == st.Label:
				return signal{}
			}
			return s
		}
	case *If:
		if r.eval(fr, st.Cond).(bool) {
			return r.block(fr, st.Then)
		} else if st.Else != nil {
			return r.block(fr, st.Else)
		}
	case *Let:
		var vals []any
		if c, ok := st.Value.(*Call); ok && len(st.Names) != 1 {
			vals = r.invoke(fr, c)
		} else {
			vals = []any{r.eval(fr, st.Value)}
		}
		for i, n := range st.Names {
			fr.locals[n] = vals[i]
			*declared = append(*declared, n)
		}
	case *Set:
		v := r.eval(fr, st.Value)
		if st.Reg {
			r.regs[st.Name] = v
		} else {
			fr.locals[st.Name] = v
		}
	case *Do:
		r.invoke(fr, st.Call)
	case *Exit:
		return signal{kind: sigExit, label: st.Label}
	case *Break:
		return signal{kind: sigBreak, label: st.Label}
	case *Continue:
		return signal{kind: sigContinue, label: st.Label}
	case *Return:
		vals := make([]any, len(st.Values))
		for i, x := range st.Values {
			vals[i] = r.eval(fr, x)
		}
		return signal{kind: sigReturn, values: vals}
	case *Assert:
		if !r.eval(fr, st.Cond).(bool) {
			panic(&AssertionError{Func: fr.f.Name, Msg: st.Msg})
		}
	default:
		panic(fmt.Sprintf("exec: unknown statement %T", st))
	}
	return signal{}
}

func (r *runner) invoke(fr *frame, c *Call) []any {
	args := make([]any, len(c.Args))
	for i, a := range c.Args {
		args[i] = r.eval(fr, a)
	}
	if o := r.m.Op(c.Callee); o != nil {
		res := r.mach.Call(o, args)
		if len(res) != len(o.Results) {
			panic(fmt.Sprintf("exec: machine returned %d results of %s, want %d", len(res), o.Name, len(o.Results)))
		}
		return res
	}
	return r.call(r.m.Func(c.Callee), args)
}

func (r *runner) eval(fr *frame, x Expr) any {
	switch x := x.(type) {
	case *Const:
		return x.Value
	case *Local:
		return fr.locals[x.Name]
	case *Reg:
		return r.regs[x.Name]
	case *Feature:
		for _, f := range fr.f.Features {
			if f == x.Name {
				return true
			}
		}
		return false
	case *Unary:
		v := r.eval(fr, x.X)
		if x.Op == "!" {
			return !v.(bool)
		}
		switch v := v.(type) {
		case int64:
			return -v
		case int32:
			return -v
		case uint32:
			return -v
		case uint64:
			return -v
		}
	case *Binary:
		return r.binary(fr, x)
	case *Convert:
		return convert(r.eval(fr, x.X), x.To)
	case *Call:
		return r.invoke(fr, x)[0]
	}
	panic(fmt.Sprintf("exec: cannot evaluate %T", x))
}

// convert converts an integer value to the representation of t, wrapping at its width.
func convert(v any, t Type) any {
	var i int64
	switch v := v.(type) {
	case int64:
		i = v
	case int32:
		i = int64(v)
	case uint32:
		i = int64(v)
	case uint64:
		i = int64(v)
	}
	switch t.Kind {
	case KI32:
		return int32(i)
	case KU32:
		return uint32(i)
	case KU64:
		return uint64(i)
	}
	return i
}

func (r *runner) binary(fr *frame, x *Binary) any {
	switch x.Op {
	case "&&":
		return r.eval(fr, x.X).(bool) && r.eval(fr, x.Y).(bool)
	case "||":
		return r.eval(fr, x.X).(bool) || r.eval(fr, x.Y).(bool)
	}
	a, b := r.eval(fr, x.X), r.eval(fr, x.Y)
	switch x.Op {
	case "==":
		return a == b
	case "!=":
		return a != b
	}
	switch a := a.(type) {
	case int64:
		return intOp(x.Op, a, b.(int64))
	case int32:
		return intOp(x.Op, a, b.(int32))
	case uint32:
		return intOp(x.Op, a, b.(uint32))
	case uint64:
		return intOp(x.Op, a, b.(uint64))
	}
	panic(fmt.Sprintf("exec: operator %s on %T", x.Op, a))
}

// intOp applies an operator to two integers of one type; arithmetic wraps at its width.
func intOp[T int64 | int32 | uint32 | uint64](op string, a, b T) any {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "+":
		return a + b
	case "-":
		return a - b
	case "*":
		return a * b
	case "/":
		return a / b
	case "%":
		return a % b
	case "&":
		return a & b
	case "|":
		return a | b
	case "^":
		return a ^ b
	case "<<":
		return a << b
	case ">>":
		return a >> b
	}
	panic("exec: unknown operator " + op)
}
