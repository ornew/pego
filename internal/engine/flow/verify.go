package flow

import (
	"fmt"
	"slices"
)

// Verify checks the invariants of the function f that hold from lowering on:
//
//   - an Exit names an enclosing block, the innermost enclosing loop or the function's Fail label;
//     a Continue names the innermost enclosing loop; every block and loop has a label of its own.
//     A loop is left or continued only from its own body, outside nested loops, as a break or
//     continue without a label does;
//   - every variable read or assigned is an entry variable of f or one of its Vars, each listed
//     once; a local function has the entry variables, a framed one none, and its Cut is one of its
//     Vars;
//   - a Set has as many places as values, or one value with that many results (Results);
//   - a loop without a bound (Counter or While) can be left: its body exits to it;
//   - a function lowered with work controls (Work) charges a step before it consumes input and in each
//     iteration of a loop without a Counter (verifyWork).
//
// Variables are declared for the whole function, so that any statement may exit to any enclosing
// label: the placement of each slot in the innermost block that holds its uses comes with slot
// allocation.
func Verify(f *Func) error {
	v := &verifier{f: f, vars: map[*Var]bool{}, labels: map[*Label]bool{f.Fail: true}}
	for _, x := range []*Var{f.Start, f.Result, f.Ctx} {
		if f.Frame != (x == nil) || x != nil && !x.Entry {
			return fmt.Errorf("%s: entry variable missing, not marked or in a framed function", f.Name)
		}
		if x != nil {
			v.vars[x] = true
		}
	}
	for _, x := range f.Vars {
		if v.vars[x] || x.Entry {
			return fmt.Errorf("%s: variable %s listed twice or as an entry variable", f.Name, x.Name)
		}
		v.vars[x] = true
	}
	if !f.Frame && (f.Value != nil || f.Cut != nil) {
		return fmt.Errorf("%s: value or cut flag of a local function", f.Name)
	}
	if f.Cut != nil {
		if err := v.use(f.Cut); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	if err := v.stmts(f.Body); err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	if f.Value != nil {
		if err := v.expr(f.Value); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	if f.Work {
		return verifyWork(f)
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
		if v.f.Document {
			if err := documentRead(v.f, s); err != nil {
				return err
			}
		}
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
		if slices.Contains(v.loops, s.To) && s.To != v.loops[len(v.loops)-1] {
			return fmt.Errorf("exit to a loop that is not the innermost one")
		}
	case *Continue:
		if !slices.Contains(v.loops, s.To) {
			return fmt.Errorf("continue of a loop that does not enclose it")
		}
		if s.To != v.loops[len(v.loops)-1] {
			return fmt.Errorf("continue of a loop that is not the innermost one")
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
	case *StreamStart:
		if !v.f.Node || !v.f.Frame {
			return fmt.Errorf("%s: stream start outside a framed Node function", v.f.Name)
		}
	case *EmitStream:
		if !v.f.Node || !v.f.Frame {
			return fmt.Errorf("%s: stream element outside a framed Node function", v.f.Name)
		}
		return v.expr(s.X)
	case *Append:
		if err := v.use(s.Vals); err != nil {
			return err
		}
		return v.expr(s.X)
	case *DropCreated:
		return v.expr(s.C)
	case *Unfresh:
		return v.expr(s.X)
	case *Reset:
		return v.expr(s.M)
	case *SetCapture:
		return v.expr(s.X)
	case *ClearVals:
		return v.use(s.F)
	case *DropTrail:
		if v.f.Node {
			return fmt.Errorf("%s: frame trail in a Node function", v.f.Name)
		}
		return v.expr(s.M)
	case *FreeFrame:
		if v.f.Node {
			return fmt.Errorf("%s: frame release in a Node function", v.f.Name)
		}
		return v.use(s.F)
	case *AppendError:
		return v.expr(s.X)
	case *Step, *Silent, *SetWhere, *DeclareNames, *Comment, *ObserveSkip:
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
		if err := v.runtime(x); err != nil {
			return err
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

// runtime checks that the value operations of x belong to the runtime of the function: a Node
// function builds NewTerminalNode and NewBranchNode and has no typed value operation, and a typed
// function has no Node one.
func (v *verifier) runtime(x Expr) error {
	var typed, node string
	// Pred, Bind and the other expressions are valid in both runtimes: the printer writes each with the
	// context of its table. The typed value operations listed here are the ones the runtimes differ in.
	switch x := x.(type) {
	case *NewMatch:
		typed = "match"
	case *NewNode:
		typed = "node"
	case *ErrorValue:
		typed = "error value"
	case *FreshNode:
		typed = "fresh node"
	case *ConvertResult:
		typed = "result conversion"
	case *UseCtx, *CtxFinish, *Eval:
		typed = "action context"
	case *AttachCaptures:
		// The Node runtime attaches the captures of a frame; the values of local variables are the
		// typed runtime's form.
		if v.f.Node && x.Vals != nil {
			typed = "capture values of local variables"
		}
	case *NewTerminalNode:
		node = "terminal"
	case *NewBranchNode:
		node = "branch"
	}
	switch {
	case typed != "" && v.f.Node:
		return fmt.Errorf("%s: typed %s in a Node function", v.f.Name, typed)
	case node != "" && !v.f.Node:
		return fmt.Errorf("%s: Node %s in a typed function", v.f.Name, node)
	}
	var err error
	Subexprs(x, func(y Expr) {
		if err == nil {
			err = v.runtime(y)
		}
	})
	return err
}

// documentRead checks the condition of the If s in a function lowered for Documents: a read of the input in
// place (HasLit) is the whole condition, and the alternative is the recording primitive (MatchLit), which the
// target takes when the parse is a Document's; otherwise a Document parse would read without recording how far
// it examined.
func documentRead(f *Func, s *If) error {
	inPlace := false
	var walk func(x Expr)
	walk = func(x Expr) {
		if _, ok := x.(*HasLit); ok {
			inPlace = true
		}
		Subexprs(x, walk)
	}
	walk(s.Cond)
	if !inPlace {
		return nil
	}
	if _, ok := s.Cond.(*HasLit); !ok {
		return fmt.Errorf("%s: an in-place read is part of a larger condition in a Document function", f.Name)
	}
	if !containsMatchLit(s.Else) {
		return fmt.Errorf("%s: an in-place read without the recording primitive as its alternative in a Document function", f.Name)
	}
	return nil
}

func containsMatchLit(ss []Stmt) bool {
	found := false
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch s := s.(type) {
			case *Set:
				for _, x := range s.X {
					if _, ok := x.(*MatchLit); ok {
						found = true
					}
				}
			case *If:
				if s.Init != nil {
					walk([]Stmt{s.Init})
				}
				walk(s.Then)
				walk(s.Else)
			case *Block:
				walk(s.Body)
			case *Loop:
				walk(s.Body)
			}
		}
	}
	walk(ss)
	return found
}
