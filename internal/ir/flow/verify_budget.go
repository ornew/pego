package flow

import (
	"fmt"
	"reflect"
)

// verifySpend checks the budget-charging invariants of a function lowered with Options.Budget, which keep
// the spend of a parse bounded by its limits and the context polled (the runtime polls every 256 units
// of spend). A charge is a Step, a Peek (which charges the spend of the character it reads), or a CallRule (which the runtime charges as a rule call):
//
//   - the function charges before it consumes input: before an Advance, a MatchLit, a MatchClass, a
//     SkipTerminal or a SkipClasses;
//   - every iteration of a loop without a Counter bound charges: on each path from the start of the
//     body to a Continue or to the end of the body. A repetition of a character class is lowered to a
//     loop of Peek and Advance without a Step (an iteration of such a loop charges no step),
//     so its iteration charges the spend of the character it reads, which Budget bounds.
//
// A function whose caller charged its entry (Func.Entered) starts charged. A path is followed through
// ifs, blocks and exits; a path that ends in an Exit leaves the loop or block it names with the charge
// state it has.
func verifySpend(f *Func) error {
	w := &budgetVerifier{f: f, exits: map[*Label]*exitState{}}
	w.stmts(f.Body, f.Entered)
	return w.err
}

type exitState struct{ charged bool }

type budgetVerifier struct {
	f     *Func
	exits map[*Label]*exitState // the state at the exits seen to each label so far (all charged)
	err   error
	loops []*Loop
}

func (w *budgetVerifier) fail(format string, args ...any) {
	if w.err == nil {
		w.err = fmt.Errorf("%s: spend: %s", w.f.Name, fmt.Sprintf(format, args...))
	}
}

// exprHas reports whether the expression x contains a node for which pred holds.
func exprHas(x Expr, pred func(Expr) bool) bool {
	if x == nil {
		return false
	}
	if pred(x) {
		return true
	}
	v := reflect.ValueOf(x)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	exprType := reflect.TypeOf((*Expr)(nil)).Elem()
	for i := 0; i < v.NumField(); i++ {
		fv := v.Field(i)
		switch fv.Kind() {
		case reflect.Interface:
			if fv.Type() == exprType && !fv.IsNil() && exprHas(fv.Interface().(Expr), pred) {
				return true
			}
		case reflect.Slice:
			if fv.Type().Elem() == exprType {
				for j := 0; j < fv.Len(); j++ {
					if e, ok := fv.Index(j).Interface().(Expr); ok && exprHas(e, pred) {
						return true
					}
				}
			}
		}
	}
	return false
}

func consumes(x Expr) bool {
	switch x.(type) {
	case *MatchLit, *MatchClass, *SkipTerminal, *SkipClasses:
		return true
	}
	return false
}

func calls(x Expr) bool { _, ok := x.(*CallRule); return ok }

// expr checks the expressions xs, evaluated in order, and returns the charge state after them.
func (w *budgetVerifier) expr(charged bool, xs ...Expr) bool {
	for _, x := range xs {
		if !charged && exprHas(x, consumes) {
			w.fail("an expression consumes input before a charge")
		}
		charged = charged || exprHas(x, calls)
	}
	return charged
}

func (w *budgetVerifier) exit(to *Label, charged bool) {
	if e := w.exits[to]; e != nil {
		e.charged = e.charged && charged
	} else {
		w.exits[to] = &exitState{charged: charged}
	}
}

// stmts walks ss from the state charged, and returns the state at their end and whether the end is
// reachable.
func (w *budgetVerifier) stmts(ss []Stmt, charged bool) (bool, bool) {
	for _, s := range ss {
		switch s := s.(type) {
		case *Step, *Peek:
			charged = true
		case *Set:
			charged = w.expr(charged, s.X...)
		case *If:
			if s.Init != nil {
				charged = w.expr(charged, s.Init.X...)
			}
			charged = w.expr(charged, s.Cond)
			tc, tl := w.stmts(s.Then, charged)
			ec, el := w.stmts(s.Else, charged)
			switch {
			case tl && el:
				charged = tc && ec
			case tl:
				charged = tc
			case el:
				charged = ec
			default:
				return charged, false
			}
		case *Advance:
			if !charged {
				w.fail("an Advance before a charge")
			}
			charged = w.expr(charged, s.N)
		case *Exit:
			w.exit(s.To, charged)
			return charged, false
		case *Continue:
			if l := w.loops[len(w.loops)-1]; l.Counter == nil && !charged {
				w.fail("loop %s continues without charging in its iteration", l.Label.Name)
			}
			return charged, false
		case *Block:
			bc, live := w.stmts(s.Body, charged)
			e := w.exits[s.Label]
			delete(w.exits, s.Label)
			switch {
			case live && e != nil:
				charged = bc && e.charged
			case live:
				charged = bc
			case e != nil:
				charged = e.charged
			default:
				return charged, false
			}
		case *Loop:
			pre := charged
			w.loops = append(w.loops, s)
			c := w.expr(false, s.While)
			bc, live := w.stmts(s.Body, c)
			w.loops = w.loops[:len(w.loops)-1]
			if live && s.Counter == nil && !bc {
				w.fail("loop %s ends an iteration without charging", s.Label.Name)
			}
			e := w.exits[s.Label]
			delete(w.exits, s.Label)
			// After the loop, the state is the meet of the ways out: each exit (which has charged what the
			// loop entry and the iteration charged), and the end of a loop with a bound, which may come
			// before any iteration.
			bounded := s.Counter != nil || s.While != nil
			switch {
			case e == nil && !bounded:
				return pre, false // a loop without exits does not end
			case e == nil:
				charged = pre
			case bounded:
				charged = pre
			default:
				charged = pre || e.charged
			}
		}
	}
	return charged, true
}
