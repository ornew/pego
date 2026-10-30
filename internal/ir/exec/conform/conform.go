// Package conform is the conformance harness of the parser VM (design 065). It runs conformance
// cases, Exec IR functions that call operations, in lock-step on a reference model
// (internal/ir/exec/ref) and on an implementation under test, and fails on the first call where
// they differ: in results, in which handle a result is, in whether the call ended the parse, or in
// the units the implementation charged beyond the operation's cost bound. Cases are handwritten
// (Cases) or randomized sequences that respect the preconditions (Random).
package conform

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/ops"
	"github.com/ornew/pego/internal/ir/exec/ref"
)

// Impl is an implementation under test. Values returns the implementation's Values handle for the
// conformance identifier id; two identifiers are equal values when they are equal.
type Impl interface {
	exec.Machine
	Values(id int64) any
}

// Metered is an Impl that reports the units of budget it has charged so far, so that the harness
// checks each call against the operation's cost bound.
type Metered interface {
	Used() uint64
}

// Suite is an implementation to check.
type Suite struct {
	// Name names the implementation in failures.
	Name string
	// New returns an implementation in the state at the start of a parse with the configuration.
	New func(ref.Config) Impl
	// Ops are the operations the implementation provides. Cases that call others are skipped.
	Ops []string
}

// ValuesOp is the operation that conformance cases use to make a Values handle from an identifier.
// It is not an operation of the parser VM.
const ValuesOp = "test.values"

// Module returns the pilot declarations with ValuesOp, ready for cases.
func Module() *exec.Module {
	m := ops.Pilot()
	m.AddOp(&exec.Op{Name: ValuesOp, Component: ops.Env, Params: []exec.Param{exec.P("id", exec.TInt)},
		Results:  []exec.Type{exec.THandle(ops.Values)},
		Contract: "Returns the Values handle of a conformance identifier; equal identifiers are equal values."})
	return m
}

// Case is one conformance case: a function of a module made by Module, run with Config.
type Case struct {
	Module *exec.Module
	Func   *exec.Func
	Config ref.Config
}

// Failure is a difference between the model and the implementation.
type Failure struct {
	Impl, Case string
	// Step is the index of the failing operation call, Trace the calls up to and including it.
	Step  int
	Trace []string
	Msg   string
	// Listing is the case as Exec IR.
	Listing string
}

func (f *Failure) Error() string {
	return fmt.Sprintf("%s: case %s, call %d: %s\ncalls:\n  %s\ncase:\n%s", f.Impl, f.Case, f.Step, f.Msg,
		strings.Join(f.Trace, "\n  "), f.Listing)
}

// Run runs case c on suite s. It returns nil when the case passes or calls an operation the suite
// does not provide (skipped reports which), and a *Failure otherwise.
func Run(s Suite, c Case) (skipped bool, err error) {
	if !provides(s, c.Module, c.Func, map[string]bool{}) {
		return true, nil
	}
	p := &pair{model: ref.New(c.Config), impl: s.New(c.Config),
		refOrd: map[any]int{}, implOrd: map[any]int{}, handles: map[any]*handle{}}
	fail := func(msg string) error {
		return &Failure{Impl: s.Name, Case: c.Func.Name, Step: len(p.trace), Trace: p.trace, Msg: msg,
			Listing: exec.FuncString(c.Func)}
	}
	defer func() {
		if x := recover(); x != nil {
			m, ok := x.(mismatch)
			if !ok {
				panic(x)
			}
			err = fail(string(m))
		}
	}()
	_, err = exec.Run(c.Module, c.Func, p)
	var ae *exec.AssertionError
	if errors.As(err, &ae) {
		return false, fail("assertion of the case failed on the model and the implementation: " + ae.Msg)
	}
	// An interruption agreed on by both sides ends the case.
	return false, nil
}

// provides reports whether every operation that f calls, directly or through functions, is in
// s.Ops.
func provides(s Suite, m *exec.Module, f *exec.Func, visited map[string]bool) bool {
	if visited[f.Name] {
		return true
	}
	visited[f.Name] = true
	ok := true
	walkCalls(f.Body, func(c *exec.Call) {
		switch {
		case c.Callee == ValuesOp:
		case m.Op(c.Callee) != nil:
			ok = ok && slices.Contains(s.Ops, c.Callee)
		case m.Func(c.Callee) != nil:
			ok = ok && provides(s, m, m.Func(c.Callee), visited)
		}
	})
	return ok
}

func walkCalls(b *exec.Block, visit func(*exec.Call)) {
	var expr func(exec.Expr)
	expr = func(x exec.Expr) {
		switch x := x.(type) {
		case *exec.Call:
			visit(x)
			for _, a := range x.Args {
				expr(a)
			}
		case *exec.Unary:
			expr(x.X)
		case *exec.Binary:
			expr(x.X)
			expr(x.Y)
		case *exec.Convert:
			expr(x.X)
		}
	}
	for _, st := range b.Stmts {
		switch st := st.(type) {
		case *exec.Block:
			walkCalls(st, visit)
		case *exec.Loop:
			if st.Init != nil {
				for _, x := range st.Init.Values {
					expr(x)
				}
			}
			if st.Cond != nil {
				expr(st.Cond)
			}
			if st.Post != nil {
				for _, x := range st.Post.Values {
					expr(x)
				}
			}
			walkCalls(st.Body, visit)
		case *exec.If:
			if st.Init != nil {
				for _, x := range st.Init.Values {
					expr(x)
				}
			}
			expr(st.Cond)
			walkCalls(st.Then, visit)
			if st.Else != nil {
				walkCalls(st.Else, visit)
			}
		case *exec.Let:
			expr(st.Value)
		case *exec.Set:
			for _, x := range st.Values {
				expr(x)
			}
		case *exec.Do:
			expr(st.Call)
		case *exec.Return:
			for _, x := range st.Values {
				expr(x)
			}
		case *exec.Assert:
			expr(st.Cond)
		}
	}
}

// mismatch is the panic value of a difference found during a call.
type mismatch string

// handle is a handle as the interpreter sees it: the model's and the implementation's.
type handle struct{ ref, impl any }

func (h *handle) String() string { return fmt.Sprintf("h%p", h) }

// pair is the machine that runs each call on the model and the implementation.
type pair struct {
	model *ref.Model
	impl  Impl
	trace []string
	// refOrd and implOrd number the handles of each side in the order they first appear; a result
	// handle must have the same number on both sides.
	refOrd, implOrd map[any]int
	handles         map[any]*handle // by the model's handle
}

// outcome is the result of one side's call.
type outcome struct {
	res         []any
	interrupted string
}

func call(m exec.Machine, o *exec.Op, args []any) (out outcome) {
	defer func() {
		if x := recover(); x != nil {
			i, ok := x.(exec.Interrupt)
			if !ok {
				panic(x)
			}
			out.interrupted = i.Reason
		}
	}()
	return outcome{res: m.Call(o, args)}
}

func (p *pair) Call(o *exec.Op, args []any) []any {
	refArgs, implArgs := make([]any, len(args)), make([]any, len(args))
	shown := make([]string, len(args))
	for i, a := range args {
		if h, ok := a.(*handle); ok {
			refArgs[i], implArgs[i] = h.ref, h.impl
			shown[i] = fmt.Sprintf("#%d", p.refOrd[h.ref])
		} else {
			refArgs[i], implArgs[i] = a, a
			shown[i] = fmt.Sprint(a)
		}
	}
	line := fmt.Sprintf("%s(%s)", o.Name, strings.Join(shown, ", "))
	p.trace = append(p.trace, line)
	if o.Name == ValuesOp {
		id := args[0].(int64)
		return []any{p.wrap(p.model.Values(id), p.impl.Values(id))}
	}
	if !p.model.Allows(o, refArgs) {
		panic(mismatch("the case breaks the precondition of " + o.Name))
	}
	bound := int64(o.Cost.Const)
	for _, t := range o.Cost.Terms {
		bound += int64(t.Coef) * p.model.Quantity(t.Quantity, o, refArgs)
	}
	meter, metered := p.impl.(Metered)
	var before uint64
	if metered {
		before = meter.Used()
	}
	want := call(p.model, o, refArgs)
	got := call(p.impl, o, implArgs)
	charges := metered && o.Name != "budget.charge"
	if charges && want.interrupted == "" && got.interrupted == "limit" {
		// The implementation's own charges may exhaust the budget inside the operation, provided
		// that charges within the bound could.
		if !p.model.Exceeds(uint64(bound)) {
			panic(mismatch(fmt.Sprintf("interrupted by the limit, but charges within the bound %s = %d would not exceed it", o.Cost, bound)))
		}
		p.trace[len(p.trace)-1] += " -> interrupted inside: limit"
		panic(exec.Interrupt{Reason: "limit"})
	}
	if want.interrupted != got.interrupted {
		panic(mismatch(fmt.Sprintf("interrupted: model %q, implementation %q", want.interrupted, got.interrupted)))
	}
	if metered && want.interrupted == "" {
		used := meter.Used() - before
		if used > uint64(bound) {
			panic(mismatch(fmt.Sprintf("charged %d units, more than the bound %s = %d", used, o.Cost, bound)))
		}
		if charges && p.model.Absorb(used) {
			panic(mismatch(fmt.Sprintf("charged %d units past the limit without interrupting", used)))
		}
	}
	if want.interrupted != "" {
		p.trace[len(p.trace)-1] += " -> interrupted: " + want.interrupted
		panic(exec.Interrupt{Reason: want.interrupted})
	}
	res := make([]any, len(want.res))
	shown = shown[:0]
	for i, w := range want.res {
		g := got.res[i]
		if o.Results[i].Kind == exec.KHandle {
			// A handle that is not valid (the entry of a failed lookup) is not compared.
			if found := i+1 < len(want.res) && o.Results[i+1] == exec.TBool && !want.res[i+1].(bool); found {
				res[i] = &handle{}
				shown = append(shown, "_")
				continue
			}
			rn, in := ordinal(p.refOrd, w), ordinal(p.implOrd, g)
			if rn != in {
				panic(mismatch(fmt.Sprintf("result %d is handle #%d on the model and #%d on the implementation", i, rn, in)))
			}
			res[i] = p.wrap(w, g)
			shown = append(shown, fmt.Sprintf("#%d", rn))
			continue
		}
		if w != g {
			panic(mismatch(fmt.Sprintf("result %d: model %v, implementation %v", i, w, g)))
		}
		res[i] = w
		shown = append(shown, fmt.Sprint(w))
	}
	if len(shown) > 0 {
		p.trace[len(p.trace)-1] += " -> " + strings.Join(shown, ", ")
	}
	return res
}

func ordinal(m map[any]int, h any) int {
	n, ok := m[h]
	if !ok {
		n = len(m) + 1
		m[h] = n
	}
	return n
}

func (p *pair) wrap(r, i any) *handle {
	ordinal(p.refOrd, r)
	ordinal(p.implOrd, i)
	h := p.handles[r]
	if h == nil {
		h = &handle{ref: r, impl: i}
		p.handles[r] = h
	}
	return h
}
