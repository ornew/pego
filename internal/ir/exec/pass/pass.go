// Package pass holds the passes of Exec IR (design 065): transformations of functions that keep
// their behavior and that every backend prints the result of.
//
// Specialize makes the copy of a function for one set of features, in which every feature is a
// constant: the hand-written variants of one mechanism for each build (ordinary, budget-controlled,
// observed, node or typed values) become specializations of one composition. Simplify folds
// constants and removes code that cannot run, which is what makes a specialization lean.
package pass

import (
	"fmt"
	"slices"

	"github.com/ornew/zeroparse/internal/ir/exec"
)

// Specialize returns a copy of the function f of the module m named name and specialized for the
// features on, which must be features of m: each Feature expression becomes the constant true if
// its feature is in on and false otherwise, and the copy is simplified (Simplify). The copy's
// Features are on, so that it may call the operations of those features and no others.
func Specialize(m *exec.Module, f *exec.Func, name string, on []string) (*exec.Func, error) {
	for _, ft := range on {
		if !slices.Contains(m.Features, ft) {
			return nil, fmt.Errorf("specialize %s: undeclared feature %s", f.Name, ft)
		}
	}
	c := Clone(f)
	c.Name, c.Features = name, slices.Clone(on)
	c.Body = mapBlock(c.Body, func(x exec.Expr) exec.Expr {
		if ft, ok := x.(*exec.Feature); ok {
			return &exec.Const{Type: exec.TBool, Value: slices.Contains(on, ft.Name)}
		}
		return x
	})
	Simplify(c)
	return c, nil
}

// Clone returns a deep copy of f.
func Clone(f *exec.Func) *exec.Func {
	c := *f
	c.Params = slices.Clone(f.Params)
	c.Results = slices.Clone(f.Results)
	c.Locals = slices.Clone(f.Locals)
	c.Features = slices.Clone(f.Features)
	c.Body = mapBlock(f.Body, func(x exec.Expr) exec.Expr { return x })
	return &c
}

// mapBlock returns a copy of b in which every expression is replaced by what fn returns for it,
// after its subexpressions are replaced. Immediate operands are shared, not copied.
func mapBlock(b *exec.Block, fn func(exec.Expr) exec.Expr) *exec.Block {
	if b == nil {
		return nil
	}
	var x func(exec.Expr) exec.Expr
	x = func(e exec.Expr) exec.Expr {
		switch e := e.(type) {
		case nil:
			return nil
		case *exec.Const:
			c := *e
			return fn(&c)
		case *exec.Local:
			c := *e
			return fn(&c)
		case *exec.Reg:
			c := *e
			return fn(&c)
		case *exec.Feature:
			c := *e
			return fn(&c)
		case *exec.Unary:
			return fn(&exec.Unary{Op: e.Op, X: x(e.X)})
		case *exec.Binary:
			return fn(&exec.Binary{Op: e.Op, X: x(e.X), Y: x(e.Y)})
		case *exec.Convert:
			return fn(&exec.Convert{To: e.To, X: x(e.X)})
		case *exec.Call:
			return fn(call(e, x))
		}
		panic(fmt.Sprintf("pass: unknown expression %T", e))
	}
	set := func(s *exec.Set) *exec.Set {
		if s == nil {
			return nil
		}
		c := &exec.Set{Dst: slices.Clone(s.Dst)}
		for _, v := range s.Values {
			c.Values = append(c.Values, x(v))
		}
		return c
	}
	var blk func(*exec.Block) *exec.Block
	blk = func(b *exec.Block) *exec.Block {
		if b == nil {
			return nil
		}
		out := &exec.Block{Label: b.Label}
		for _, st := range b.Stmts {
			var n exec.Stmt
			switch st := st.(type) {
			case *exec.Block:
				n = blk(st)
			case *exec.Loop:
				n = &exec.Loop{Label: st.Label, Init: set(st.Init), Cond: x(st.Cond), Post: set(st.Post), Body: blk(st.Body)}
			case *exec.If:
				n = &exec.If{Init: set(st.Init), Cond: x(st.Cond), Then: blk(st.Then), Else: blk(st.Else)}
			case *exec.Let:
				n = &exec.Let{Names: slices.Clone(st.Names), Value: x(st.Value)}
			case *exec.Set:
				n = set(st)
			case *exec.Do:
				n = &exec.Do{Call: call(st.Call, x)}
			case *exec.Return:
				r := &exec.Return{}
				for _, v := range st.Values {
					r.Values = append(r.Values, x(v))
				}
				n = r
			case *exec.Assert:
				n = &exec.Assert{Cond: x(st.Cond), Msg: st.Msg}
			case *exec.Exit:
				c := *st
				n = &c
			case *exec.Break:
				c := *st
				n = &c
			case *exec.Continue:
				c := *st
				n = &c
			case *exec.Comment:
				c := *st
				n = &c
			default:
				panic(fmt.Sprintf("pass: unknown statement %T", st))
			}
			out.Stmts = append(out.Stmts, n)
		}
		return out
	}
	return blk(b)
}

func call(c *exec.Call, x func(exec.Expr) exec.Expr) *exec.Call {
	n := &exec.Call{Callee: c.Callee, Imm: c.Imm}
	for _, a := range c.Args {
		n.Args = append(n.Args, x(a))
	}
	return n
}

// Simplify folds the constant expressions of f and removes the code that cannot run, in place:
//
//   - an operator on constants is replaced by its value (a division by zero is kept, so that it
//     fails where it ran), "!" of a constant, and "&&" and "||" with a constant operand that decides
//     them or drops out (an operand that is dropped must have no call);
//   - an If with a constant condition is replaced by its Init and the branch that runs; a Loop whose
//     condition is the constant false by its Init;
//   - statements after an Exit, Break, Continue or Return in the same block are removed;
//   - an unlabeled block that declares no local is spliced into its parent, and a block label that
//     no Exit names is removed.
func Simplify(f *exec.Func) {
	f.Body = mapBlock(f.Body, fold)
	f.Body.Stmts = prune(f.Body.Stmts)
	used := map[string]bool{}
	exec.Walk(f.Body, func(s exec.Stmt) {
		if e, ok := s.(*exec.Exit); ok {
			used[e.Label] = true
		}
	}, nil)
	exec.Walk(f.Body, func(s exec.Stmt) {
		if b, ok := s.(*exec.Block); ok && !used[b.Label] {
			b.Label = ""
		}
	}, nil)
	f.Body.Stmts = splice(f.Body.Stmts)
}

// constant returns the value of x if it is a constant.
func constant(x exec.Expr) (any, bool) {
	c, ok := x.(*exec.Const)
	if !ok || c.Type.Kind == exec.KHandle {
		return nil, false
	}
	return c.Value, true
}

// pure reports whether x has no call.
func pure(x exec.Expr) bool {
	p := true
	exec.Walk(&exec.Block{Stmts: []exec.Stmt{&exec.Assert{Cond: x}}}, nil, func(e exec.Expr) {
		if _, ok := e.(*exec.Call); ok {
			p = false
		}
	})
	return p
}

func boolConst(v bool) exec.Expr { return &exec.Const{Type: exec.TBool, Value: v} }

// fold folds the expression x, whose subexpressions are folded.
func fold(x exec.Expr) exec.Expr {
	switch x := x.(type) {
	case *exec.Unary:
		v, ok := constant(x.X)
		if !ok {
			return x
		}
		if x.Op == "!" {
			return boolConst(!v.(bool))
		}
		c := x.X.(*exec.Const)
		return &exec.Const{Type: c.Type, Value: exec.Apply("-", exec.ConvertValue(int64(0), c.Type), v)}
	case *exec.Binary:
		a, aok := constant(x.X)
		b, bok := constant(x.Y)
		switch x.Op {
		case "&&", "||":
			decide := x.Op == "||" // the value of an operand that decides the result
			switch {
			case aok && a.(bool) == decide:
				return boolConst(decide)
			case aok:
				return x.Y
			case bok && b.(bool) != decide:
				return x.X
			case bok && pure(x.X):
				return boolConst(decide)
			}
			return x
		}
		if !aok || !bok {
			return x
		}
		if (x.Op == "/" || x.Op == "%") && exec.Apply("==", b, exec.ConvertValue(int64(0), x.Y.(*exec.Const).Type)).(bool) {
			return x
		}
		v := exec.Apply(x.Op, a, b)
		t := x.X.(*exec.Const).Type
		if _, isBool := v.(bool); isBool {
			t = exec.TBool
		}
		return &exec.Const{Type: t, Value: v}
	case *exec.Convert:
		if v, ok := constant(x.X); ok {
			return &exec.Const{Type: x.To, Value: exec.ConvertValue(v, x.To)}
		}
	}
	return x
}

// prune replaces Ifs and Loops with constant conditions and removes unreachable statements, in a
// list of statements and its nested blocks.
func prune(ss []exec.Stmt) []exec.Stmt {
	var out []exec.Stmt
	for _, st := range ss {
		switch s := st.(type) {
		case *exec.Block:
			s.Stmts = prune(s.Stmts)
		case *exec.Loop:
			if v, ok := constant(s.Cond); ok && !v.(bool) {
				if s.Init != nil {
					out = append(out, s.Init)
				}
				continue
			}
			if v, ok := constant(s.Cond); ok && v.(bool) {
				s.Cond = nil
			}
			s.Body.Stmts = prune(s.Body.Stmts)
		case *exec.If:
			s.Then.Stmts = prune(s.Then.Stmts)
			if s.Else != nil {
				s.Else.Stmts = prune(s.Else.Stmts)
			}
			if v, ok := constant(s.Cond); ok {
				if s.Init != nil {
					out = append(out, s.Init)
				}
				if v.(bool) {
					out = append(out, s.Then)
				} else if s.Else != nil {
					out = append(out, s.Else)
				}
				continue
			}
		}
		out = append(out, st)
		switch st.(type) {
		case *exec.Exit, *exec.Break, *exec.Continue, *exec.Return:
			return out
		}
	}
	return out
}

// splice splices unlabeled blocks that declare no local into the list, in it and in its nested
// blocks, and removes empty unlabeled blocks.
func splice(ss []exec.Stmt) []exec.Stmt {
	var out []exec.Stmt
	for _, st := range ss {
		switch s := st.(type) {
		case *exec.Block:
			s.Stmts = splice(s.Stmts)
			if s.Label == "" && !declares(s) {
				out = append(out, s.Stmts...)
				continue
			}
		case *exec.Loop:
			s.Body.Stmts = splice(s.Body.Stmts)
		case *exec.If:
			s.Then.Stmts = splice(s.Then.Stmts)
			if s.Else != nil {
				s.Else.Stmts = splice(s.Else.Stmts)
				if len(s.Else.Stmts) == 0 {
					s.Else = nil
				}
			}
		}
		out = append(out, st)
	}
	return out
}

// declares reports whether the block declares a local directly.
func declares(b *exec.Block) bool {
	for _, st := range b.Stmts {
		if _, ok := st.(*exec.Let); ok {
			return true
		}
	}
	return false
}
