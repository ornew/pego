package flow

import (
	"fmt"
	"slices"
)

// Verify checks the invariants of the function f that hold from lowering on:
//
//   - an Exit names an enclosing block or loop, or the function's Fail label; a Continue names an
//     enclosing loop; every block and loop has a label of its own;
//   - every variable read or assigned is an entry variable of f or one of its Vars, each listed
//     once;
//   - a Set has as many places as values, or one value with that many results (Results);
//   - a loop without a bound (Counter or While) can be left: its body exits to it.
//
// Variables are declared for the whole function, so that any statement may exit to any enclosing
// label: the placement of each slot in the innermost block that holds its uses comes with slot
// allocation.
func Verify(f *Func) error {
	v := &verifier{f: f, vars: map[*Var]bool{}, labels: map[*Label]bool{f.Fail: true}}
	for _, x := range []*Var{f.Start, f.Result, f.Ctx} {
		if x == nil || !x.Entry {
			return fmt.Errorf("%s: entry variable missing or not marked", f.Name)
		}
		v.vars[x] = true
	}
	for _, x := range f.Vars {
		if v.vars[x] || x.Entry {
			return fmt.Errorf("%s: variable %s listed twice or as an entry variable", f.Name, x.Name)
		}
		v.vars[x] = true
	}
	if err := v.stmts(f.Body); err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	return nil
}

type verifier struct {
	f      *Func
	vars   map[*Var]bool
	labels map[*Label]bool // labels made so far
	// open holds the enclosing blocks' and loops' labels, innermost last, and loops those of loops.
	open  []*Label
	loops []*Label
}

func (v *verifier) stmts(ss []Stmt) error {
	for _, s := range ss {
		if err := v.stmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (v *verifier) label(l *Label) error {
	if l == nil || v.labels[l] {
		return fmt.Errorf("block or loop without a label of its own")
	}
	v.labels[l] = true
	return nil
}

func (v *verifier) stmt(s Stmt) error {
	switch s := s.(type) {
	case *Block:
		if err := v.label(s.Label); err != nil {
			return err
		}
		v.open = append(v.open, s.Label)
		err := v.stmts(s.Body)
		v.open = v.open[:len(v.open)-1]
		return err
	case *Loop:
		if err := v.label(s.Label); err != nil {
			return err
		}
		if s.Counter != nil {
			if err := v.use(s.Counter); err != nil {
				return err
			}
		}
		if s.While != nil {
			if err := v.expr(s.While); err != nil {
				return err
			}
		}
		if s.Counter == nil && s.While == nil && !exits(s.Body, s.Label) {
			return fmt.Errorf("unbounded loop that never exits")
		}
		v.open, v.loops = append(v.open, s.Label), append(v.loops, s.Label)
		err := v.stmts(s.Body)
		v.open, v.loops = v.open[:len(v.open)-1], v.loops[:len(v.loops)-1]
		return err
	case *If:
		if s.Init != nil {
			if err := v.stmt(s.Init); err != nil {
				return err
			}
		}
		if err := v.expr(s.Cond); err != nil {
			return err
		}
		if err := v.stmts(s.Then); err != nil {
			return err
		}
		return v.stmts(s.Else)
	case *Exit:
		if s.To != v.f.Fail && !slices.Contains(v.open, s.To) {
			return fmt.Errorf("exit to %q, which does not enclose it", s.To.Name)
		}
	case *Continue:
		if !slices.Contains(v.loops, s.To) {
			return fmt.Errorf("continue of a loop that does not enclose it")
		}
	case *Set:
		n := len(s.X)
		if n == 1 {
			n = Results(s.X[0])
		}
		if n != len(s.Dst) {
			return fmt.Errorf("assignment of %d values to %d places", n, len(s.Dst))
		}
		for _, d := range s.Dst {
			if x, ok := d.(*Var); ok {
				if err := v.use(x); err != nil {
					return err
				}
			}
		}
		return v.expr(s.X...)
	case *Inc:
		return v.use(s.V)
	case *Peek:
		for _, x := range []*Var{s.Ch, s.Size, s.OK} {
			if x != nil {
				if err := v.use(x); err != nil {
					return err
				}
			}
		}
		if s.OK == nil {
			return fmt.Errorf("peek without a variable for whether there is a character")
		}
	case *Advance:
		return v.expr(s.N)
	case *Expect:
		return v.expr(s.At)
	case *Restore:
		return v.expr(s.Pos, s.Rec)
	case *MergeExpected:
		return v.expr(s.Far, s.Exps)
	case *MergeRenamed:
		return v.expr(s.Far, s.Exps)
	case *PushKid:
		return v.expr(s.X)
	case *DropKids:
		return v.expr(s.Base)
	case *Append:
		if err := v.use(s.Vals); err != nil {
			return err
		}
		return v.expr(s.X)
	case *DropCreated:
		return v.expr(s.C)
	case *Unfresh:
		return v.expr(s.X)
	case *Step, *Silent, *SetWhere:
	default:
		return fmt.Errorf("unknown statement %T", s)
	}
	return nil
}

func (v *verifier) use(x *Var) error {
	if !v.vars[x] {
		return fmt.Errorf("variable %s is not declared", x.Name)
	}
	return nil
}

func (v *verifier) expr(xs ...Expr) error {
	var err error
	for _, x := range xs {
		if x == nil {
			return fmt.Errorf("missing expression")
		}
		Uses(x, func(u *Var) {
			if err == nil {
				err = v.use(u)
			}
		})
	}
	return err
}

// exits reports whether the statements ss contain an Exit to the label l.
func exits(ss []Stmt, l *Label) bool {
	found := false
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch s := s.(type) {
			case *Exit:
				found = found || s.To == l
			case *Block:
				walk(s.Body)
			case *Loop:
				walk(s.Body)
			case *If:
				walk(s.Then)
				walk(s.Else)
			}
		}
	}
	walk(ss)
	return found
}
