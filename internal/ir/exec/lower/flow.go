// Package lower lowers the Flow IR of grammar code to Exec IR (design 065), so that the backends of
// Exec IR print the rules of a grammar as they print the compositions of the parser VM.
//
// The structure of a Flow IR function (blocks, loops, branches, exits, assignments and the
// arithmetic and logic of its conditions) becomes the same structure of Exec IR, its variables
// become locals and its registers registers. Each other primitive of Flow IR (matching a literal,
// building a value, recording an expectation, calling a rule) becomes the call of a transitional
// operation of its kind, which holds the primitive in Call.Imm: the target prints it as the
// hand-written runtime's helper, as Flow IR's printers do, until the stage that migrates the
// mechanism. The arguments of such a call are the locals the primitive reads, so that passes see
// its reads. An operation is declared once per kind and shape of its call.
package lower

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/flow"
)

// Component is the state component of the operations that stand for Flow IR primitives: the state
// of the hand-written runtime.
const Component = "runtime"

// Names of the registers of lowered functions. The registers of Flow IR that hold handles (the
// environment and the frame) are read and set by the operations of the kinds flow.Read and
// flow.SetReg, since a register of Exec IR holds a scalar.
const (
	Pos = "pos"
	Cut = "cut"
)

// Any is the handle type of a result that a lowered function discards.
const Any = "any"

// Lowered is a Flow IR function lowered to Exec IR.
type Lowered struct {
	// Module declares the registers, handle types and operations that Func calls, and holds Func.
	Module *exec.Module
	Func   *exec.Func
	// Vars are the locals of Func that are variables of the Flow IR function (Func.Vars); the other
	// locals are its entry variables, which the caller declares.
	Vars []string
}

// Flow lowers the function f. The body of the lowered function is a block labeled with f's Fail
// label, which an Exit to Fail leaves; the caller prints what follows success and failure. A framed
// function ends its block by publishing its cut flag (Func.Cut) and returning its value and true;
// after the block, it publishes the cut flag again and returns the null value and false.
func Flow(f *flow.Func) (*Lowered, error) {
	l := &lowerer{m: exec.NewModule("flow " + f.Name), shapes: map[string]string{}, kinds: map[string]int{},
		types: map[*flow.Var]exec.Type{}, loops: map[*flow.Label]bool{}, labels: map[*flow.Label]string{}}
	l.m.AddComponent(Component, "The state of the hand-written runtime.")
	l.m.AddRegister(Pos, exec.TInt, "The position.")
	l.handle(Any)
	l.m.AddRegister(Cut, exec.TBool, "The cut flag of the scope the runtime entered last.")
	fn := &exec.Func{Name: f.Name}
	out := &Lowered{Module: l.m, Func: fn}
	for _, v := range f.Vars {
		t := l.varType(v.Type)
		l.types[v] = t
		fn.Locals = append(fn.Locals, exec.P(v.Name, t))
		out.Vars = append(out.Vars, v.Name)
	}
	// The entry variables, which the caller declares.
	for _, v := range []*flow.Var{f.Start, f.Result, f.Ctx} {
		if v != nil && l.types[v] == (exec.Type{}) {
			t := l.varType(v.Type)
			l.types[v] = t
			fn.Locals = append(fn.Locals, exec.P(v.Name, t))
		}
	}
	body := &exec.Block{Label: l.label(f.Fail), Stmts: l.stmts(f.Body)}
	fn.Body = body
	if f.Frame {
		value := l.handle(flow.TypeValue.String())
		fn.Results = []exec.Type{value, exec.TBool}
		publish := func() []exec.Stmt {
			if f.Cut == nil {
				return nil
			}
			return []exec.Stmt{&exec.Set{Dst: []exec.Place{{Name: Cut, Reg: true}}, Values: []exec.Expr{exec.L(f.Cut.Name)}}}
		}
		var v exec.Expr = exec.Null(value)
		if f.Value != nil {
			v = l.expr(f.Value, value)
		}
		body.Stmts = append(append(body.Stmts, publish()...), &exec.Return{Values: []exec.Expr{v, exec.BoolC(true)}})
		fn.Body = &exec.Block{Stmts: append(append([]exec.Stmt{body}, publish()...),
			&exec.Return{Values: []exec.Expr{exec.Null(value), exec.BoolC(false)}})}
	}
	if l.err != nil {
		return nil, fmt.Errorf("lower %s: %w", f.Name, l.err)
	}
	l.m.Funcs = append(l.m.Funcs, fn)
	if err := exec.Verify(l.m); err != nil {
		return nil, fmt.Errorf("lower %s: %w", f.Name, err)
	}
	return out, nil
}

type lowerer struct {
	m *exec.Module
	// shapes maps the shape of an operation's call to the operation's name, and kinds counts the
	// shapes of each kind.
	shapes map[string]string
	kinds  map[string]int
	types  map[*flow.Var]exec.Type
	loops  map[*flow.Label]bool
	anon   int
	labels map[*flow.Label]string
	err    error
}

// handle returns the handle type name, declaring it on first use.
func (l *lowerer) handle(name string) exec.Type {
	for _, h := range l.m.Handles {
		if h.Name == name {
			return exec.THandle(name)
		}
	}
	return l.m.AddHandle(name, Component, "A "+name+" of the hand-written runtime.")
}

// varType returns the Exec IR type of a variable of the Flow IR type t.
func (l *lowerer) varType(t flow.Type) exec.Type {
	switch t {
	case flow.TypeInt:
		return exec.TInt
	case flow.TypeBool:
		return exec.TBool
	}
	return l.handle(t.String())
}

// Kind returns the name of the kind of the Flow IR primitive x, which names its operations
// ("flow.MatchLit").
func Kind(x any) string {
	if _, ok := x.(flow.Reg); ok {
		return "flow.SetReg"
	}
	return "flow." + reflect.TypeOf(x).Elem().Name()
}

// op returns a call of the operation of the primitive x with the arguments args and the results
// results, declaring the operation for the shape of the call on first use.
func (l *lowerer) op(x any, args []exec.Expr, results []exec.Type) *exec.Call {
	types := make([]exec.Type, len(args))
	for i, a := range args {
		types[i] = l.types[l.local(a)]
	}
	return l.opTyped(x, args, types, results)
}

// opTyped is op with the types of the arguments.
func (l *lowerer) opTyped(x any, args []exec.Expr, types, results []exec.Type) *exec.Call {
	kind := Kind(x)
	var params []exec.Param
	var sig []string
	for i := range args {
		t := types[i]
		params = append(params, exec.P(fmt.Sprintf("a%d", i), t))
		sig = append(sig, t.String())
	}
	var res []string
	for _, r := range results {
		res = append(res, r.String())
	}
	key := kind + "(" + strings.Join(sig, ",") + ")" + strings.Join(res, ",")
	name, ok := l.shapes[key]
	if !ok {
		l.kinds[kind]++
		name = kind
		if n := l.kinds[kind]; n > 1 {
			name = fmt.Sprintf("%s.%d", kind, n)
		}
		l.shapes[key] = name
		l.m.AddOp(&exec.Op{Name: name, Component: Component, Params: params, Results: results,
			Reads: []string{Component, Pos, Cut}, Writes: []string{Component, Pos, Cut},
			NoReturn: true, Transitional: true, Imm: "the Flow IR primitive",
			Contract: "Does what the Flow IR primitive " + strings.TrimPrefix(kind, "flow.") + " in Imm does."})
	}
	return &exec.Call{Callee: name, Args: args, Imm: x}
}

// local returns the variable that the argument a, a Local, names.
func (l *lowerer) local(a exec.Expr) *flow.Var {
	name := a.(*exec.Local).Name
	for v := range l.types {
		if v.Name == name {
			return v
		}
	}
	panic("lower: no variable " + name)
}

// uses returns the locals that the expressions read, in order, each once.
func (l *lowerer) uses(xs ...flow.Expr) []exec.Expr {
	var out []exec.Expr
	seen := map[*flow.Var]bool{}
	for _, x := range xs {
		if x == nil {
			continue
		}
		flow.Uses(x, func(v *flow.Var) {
			if !seen[v] {
				seen[v] = true
				out = append(out, exec.L(v.Name))
			}
		})
	}
	return out
}

func (l *lowerer) stmts(ss []flow.Stmt) []exec.Stmt {
	var out []exec.Stmt
	for _, s := range ss {
		out = append(out, l.stmt(s))
	}
	return out
}

func (l *lowerer) stmt(s flow.Stmt) exec.Stmt {
	switch s := s.(type) {
	case *flow.Block:
		return &exec.Block{Label: l.label(s.Label), Stmts: l.stmts(s.Body)}
	case *flow.Loop:
		l.loops[s.Label] = true
		lp := &exec.Loop{Label: l.label(s.Label)}
		switch {
		case s.Counter != nil:
			c := s.Counter.Name
			lp.Init = exec.Assign(c, exec.Int(0))
			lp.Cond = &exec.Binary{Op: "<", X: exec.L(c), Y: exec.Int(int64(s.Max))}
			lp.Post = exec.Assign(c, &exec.Binary{Op: "+", X: exec.L(c), Y: exec.Int(1)})
		case s.While != nil:
			lp.Cond = l.expr(s.While, exec.TBool)
		}
		lp.Body = &exec.Block{Stmts: l.stmts(s.Body)}
		return lp
	case *flow.If:
		st := &exec.If{Cond: l.expr(s.Cond, exec.TBool), Then: &exec.Block{Stmts: l.stmts(s.Then)}}
		if s.Init != nil {
			st.Init = l.set(s.Init)
		}
		if s.Else != nil {
			st.Else = &exec.Block{Stmts: l.stmts(s.Else)}
		}
		return st
	case *flow.Exit:
		if l.loops[s.To] {
			return &exec.Break{Label: l.label(s.To)}
		}
		return &exec.Exit{Label: l.label(s.To)}
	case *flow.Continue:
		return &exec.Continue{Label: l.label(s.To)}
	case *flow.Set:
		if st := l.set(s); st != nil {
			return st
		}
		// The assignment of a register that holds a handle.
		r := s.Dst[0].(flow.Reg)
		t := l.handleReg(r)
		return &exec.Do{Call: l.opTyped(r, []exec.Expr{l.expr(s.X[0], t)}, []exec.Type{t}, nil)}
	case *flow.Inc:
		return exec.Assign(s.V.Name, &exec.Binary{Op: "+", X: exec.L(s.V.Name), Y: exec.Int(1)})
	case *flow.Peek:
		dst := make([]exec.Place, 3)
		var res []exec.Type
		for i, v := range []*flow.Var{s.Ch, s.Size, s.OK} {
			if v != nil {
				dst[i] = exec.Place{Name: v.Name}
				res = append(res, l.types[v])
			} else {
				res = append(res, l.handle(Any))
			}
		}
		return &exec.Set{Dst: dst, Values: []exec.Expr{l.op(s, nil, res)}}
	case *flow.Append:
		// The list is assigned the list with X appended.
		args := append([]exec.Expr{exec.L(s.Vals.Name)}, l.uses(s.X)...)
		return &exec.Set{Dst: []exec.Place{{Name: s.Vals.Name}}, Values: []exec.Expr{l.op(s, dedup(args), []exec.Type{l.types[s.Vals]})}}
	case *flow.Comment:
		return &exec.Comment{Text: s.Text}
	}
	return &exec.Do{Call: l.op(s, l.stmtUses(s), nil)}
}

// dedup returns the locals args, each once.
func dedup(args []exec.Expr) []exec.Expr {
	var out []exec.Expr
	seen := map[string]bool{}
	for _, a := range args {
		if n := a.(*exec.Local).Name; !seen[n] {
			seen[n] = true
			out = append(out, a)
		}
	}
	return out
}

// stmtUses returns the locals that the primitive statement s reads.
func (l *lowerer) stmtUses(s flow.Stmt) []exec.Expr {
	switch s := s.(type) {
	case *flow.Advance:
		return l.uses(s.N)
	case *flow.Expect:
		return l.uses(s.At)
	case *flow.Restore:
		return l.uses(s.Pos, s.Rec)
	case *flow.MergeExpected:
		return l.uses(s.Far, s.Exps)
	case *flow.MergeRenamed:
		return l.uses(s.Far, s.Exps)
	case *flow.PushKid:
		return l.uses(s.X)
	case *flow.EmitStream:
		return l.uses(s.X)
	case *flow.DropKids:
		return l.uses(s.Base)
	case *flow.DropCreated:
		return l.uses(s.C)
	case *flow.Unfresh:
		return l.uses(s.X)
	case *flow.Reset:
		return l.uses(s.M)
	case *flow.SetCapture:
		return l.uses(s.X)
	case *flow.ClearVals:
		return []exec.Expr{exec.L(s.F.Name)}
	case *flow.DropTrail:
		return l.uses(s.M)
	case *flow.FreeFrame:
		return []exec.Expr{exec.L(s.F.Name)}
	case *flow.AppendError:
		return l.uses(s.X)
	case *flow.Step, *flow.Silent, *flow.StreamStart, *flow.SetWhere, *flow.DeclareNames, *flow.ObserveSkip:
		return nil
	}
	if l.err == nil {
		l.err = fmt.Errorf("statement %T", s)
	}
	return nil
}

// set lowers the assignment s.
func (l *lowerer) set(s *flow.Set) *exec.Set {
	dst := make([]exec.Place, len(s.Dst))
	types := make([]exec.Type, len(s.Dst))
	for i, d := range s.Dst {
		switch d := d.(type) {
		case *flow.Var:
			dst[i], types[i] = exec.Place{Name: d.Name}, l.types[d]
		case flow.Reg:
			if d == flow.RegEnv || d == flow.RegFrame {
				if len(s.Dst) != 1 || len(s.X) != 1 {
					l.fail(fmt.Errorf("assignment of the register %d with other places", d))
					return &exec.Set{Dst: []exec.Place{{}}, Values: []exec.Expr{exec.BoolC(false)}}
				}
				return nil
			}
			name := regName(d)
			dst[i], types[i] = exec.Place{Name: name, Reg: true}, l.regType(name)
		case *flow.Discard:
			types[i] = l.handle(Any)
		}
	}
	out := &exec.Set{Dst: dst}
	if len(s.X) == 1 && len(s.Dst) > 1 {
		// One primitive of several results.
		out.Values = []exec.Expr{l.op(s.X[0], l.uses(s.X[0]), types)}
		return out
	}
	for i, x := range s.X {
		out.Values = append(out.Values, l.expr(x, types[i]))
	}
	return out
}

// regType returns the type of the register name.
func (l *lowerer) regType(name string) exec.Type {
	for _, r := range l.m.Registers {
		if r.Name == name {
			return r.Type
		}
	}
	panic("lower: no register " + name)
}

// label returns the Exec IR label of the Flow IR label x: its name, or for a label without one
// (which nothing prints, since a printer names only the labels of blocks), a name of its own.
func (l *lowerer) label(x *flow.Label) string {
	if name, ok := l.labels[x]; ok {
		return name
	}
	name := ""
	if x != nil {
		name = x.Name
	}
	if name == "" {
		l.anon++
		name = fmt.Sprintf("loop.%d", l.anon)
	}
	l.labels[x] = name
	return name
}

func regName(r flow.Reg) string {
	if r == flow.RegCut {
		return Cut
	}
	return Pos
}

func (l *lowerer) fail(err error) {
	if l.err == nil {
		l.err = err
	}
}

// handleReg returns the handle type of the register r of Flow IR that holds a handle.
func (l *lowerer) handleReg(r flow.Reg) exec.Type {
	if r == flow.RegEnv {
		return l.handle(flow.TypeEnv.String())
	}
	return l.handle(flow.TypeFrame.String())
}

// expr lowers the expression x, of the type t.
func (l *lowerer) expr(x flow.Expr, t exec.Type) exec.Expr {
	switch x := x.(type) {
	case *flow.Use:
		return exec.L(x.V.Name)
	case *flow.Read:
		if x.R == flow.RegEnv || x.R == flow.RegFrame {
			return l.opTyped(x, nil, nil, []exec.Type{l.handleReg(x.R)})
		}
		return exec.R(regName(x.R))
	case *flow.Nil:
		return exec.Null(t)
	case *flow.Int:
		return exec.Int(int64(x.N))
	case *flow.Bool:
		return exec.BoolC(x.B)
	case *flow.Not:
		return &exec.Unary{Op: "!", X: l.expr(x.X, exec.TBool)}
	case *flow.Or:
		return &exec.Binary{Op: "||", X: l.expr(x.X, exec.TBool), Y: l.expr(x.Y, exec.TBool)}
	case *flow.Cmp:
		ot := exec.TInt
		if u, ok := x.L.(*flow.Use); ok {
			ot = l.types[u.V]
		} else if u, ok := x.R.(*flow.Use); ok {
			ot = l.types[u.V]
		}
		return &exec.Binary{Op: x.Op, X: l.expr(x.L, ot), Y: l.expr(x.R, ot)}
	}
	return l.op(x, l.uses(x), []exec.Type{t})
}
