package pass

import "github.com/ornew/pego/internal/ir/exec"

// Stores is what DeadStores finds about the locals of a function: which are never read, so that a
// backend does not declare them, and which assignments it leaves out.
type Stores struct {
	// Dead holds the locals that are not declared: nothing that stays reads them and nothing forces
	// them to stay. A backend prints a place of a dead local as the blank identifier (or the target's
	// equivalent).
	Dead map[string]bool
	// Read holds the locals that a statement that stays reads.
	Read map[string]bool
	// Named holds the locals that any reachable statement reads, including the assignments that are
	// left out.
	Named map[string]bool
	// Removed holds the assignments that are left out: pure ones whose every place is dead.
	Removed map[*exec.Set]bool
}

// DeadStores finds the stores of the function f that nothing reads, among the locals cand.
//
// Reachability follows the printers: after an Exit, a Break, a Continue or a Return the point is
// unreachable up to the end of the enclosing block; the point after a block is reachable if its
// end is or if a reachable jump exits it; the point after an If is reachable if the point before it
// is, and the point after a loop always is. Only reachable statements read. The headers of loops
// read whatever the reachability.
//
// An assignment (a Set that is a statement of a block) whose values are all pure, one value per
// place, is left out when every place is a dead local; leaving it out leaves the
// locals its values read unread, so the removal repeats until no more locals become dead. A pure
// assignment's reads of its own places (an increment) do not keep them. pure
// reports whether evaluating an expression has no effect and cannot fail on the target that prints
// the function. An assignment that is not removable keeps its values and prints dead places as
// blank. A local stays (is never dead) if it is assigned by the Init of an If or of a loop, which
// cannot be left out, or opposite a null handle in an assignment of several values, which a
// backend may not be able to print to the blank identifier.
func DeadStores(f *exec.Func, cand []string, pure func(exec.Expr) bool) *Stores {
	s := &stores{pure: pure, hard: map[string]bool{}, stuck: map[string]bool{}, named: map[string]bool{},
		labels: map[string]bool{}}
	s.block(f.Body, true)
	isCand := map[string]bool{}
	for _, c := range cand {
		isCand[c] = true
	}
	reads := map[string]bool{}
	for v := range s.named {
		reads[v] = true
	}
	dead := func(v string) bool { return v != "" && isCand[v] && !reads[v] && !s.stuck[v] }
	cut := func(st *store) bool {
		if !st.pure {
			return false
		}
		for _, d := range st.set.Dst {
			if d.Reg || d.Name == "" || !dead(d.Name) {
				return false
			}
		}
		return true
	}
	for {
		next := map[string]bool{}
		for v := range s.hard {
			next[v] = true
		}
		for _, st := range s.stores {
			if !cut(st) {
				for v := range st.uses {
					next[v] = true
				}
			}
		}
		if len(next) == len(reads) {
			break
		}
		reads = next
	}
	out := &Stores{Dead: map[string]bool{}, Read: reads, Named: s.named, Removed: map[*exec.Set]bool{}}
	for _, c := range cand {
		if dead(c) {
			out.Dead[c] = true
		}
	}
	for _, st := range s.stores {
		if cut(st) {
			out.Removed[st.set] = true
		}
	}
	return out
}

type stores struct {
	pure func(exec.Expr) bool
	// hard holds the locals read by statements that stay whatever is removed, named those read by
	// any reachable statement, and stuck those that stay declared.
	hard, named, stuck map[string]bool
	stores             []*store
	// labels holds the labels exited to from reachable points.
	labels map[string]bool
}

// store is an assignment that is left out if its places are dead.
type store struct {
	set  *exec.Set
	uses map[string]bool
	pure bool
}

// reads records the locals that x reads into the set into, and into named.
func (s *stores) reads(x exec.Expr, into map[string]bool) {
	if x == nil {
		return
	}
	exec.Walk(&exec.Block{Stmts: []exec.Stmt{&exec.Assert{Cond: x}}}, nil, func(e exec.Expr) {
		if l, ok := e.(*exec.Local); ok {
			into[l.Name] = true
			s.named[l.Name] = true
		}
	})
}

// header records the reads and the assignments of the Init or Post of a loop or an If.
func (s *stores) header(st *exec.Set) {
	if st == nil {
		return
	}
	for _, x := range st.Values {
		s.reads(x, s.hard)
	}
	for _, d := range st.Dst {
		if !d.Reg && d.Name != "" {
			s.stuck[d.Name] = true
		}
	}
}

// block walks the statements of b, the first of which is reachable if live, and reports whether
// the point after b is reachable.
func (s *stores) block(b *exec.Block, live bool) bool {
	for _, st := range b.Stmts {
		live = s.stmt(st, live)
	}
	if b.Label != "" && s.labels[b.Label] {
		live = true
	}
	return live
}

// stmt walks the statement st, reachable if live, and reports whether the point after it is.
func (s *stores) stmt(st exec.Stmt, live bool) bool {
	switch st := st.(type) {
	case *exec.Block:
		return s.block(st, live)
	case *exec.Loop:
		// The header is printed whatever the reachability.
		s.header(st.Init)
		s.reads(st.Cond, s.hard)
		s.header(st.Post)
		s.block(st.Body, live)
		return true
	case *exec.If:
		if live {
			s.header(st.Init)
			s.reads(st.Cond, s.hard)
		}
		s.block(st.Then, live)
		if st.Else != nil {
			s.block(st.Else, live)
		}
		return live
	case *exec.Set:
		if !live {
			return false
		}
		uses := map[string]bool{}
		pure := len(st.Dst) == len(st.Values)
		for i, x := range st.Values {
			s.reads(x, uses)
			pure = pure && s.pure(x)
			if c, ok := x.(*exec.Const); ok && c.Type.Kind == exec.KHandle && len(st.Dst) > 1 && len(st.Dst) == len(st.Values) {
				if d := st.Dst[i]; !d.Reg && d.Name != "" {
					s.stuck[d.Name] = true
				}
			}
		}
		if pure {
			// A pure store that reads its own place (x = x + 1) keeps nothing but itself.
			for _, d := range st.Dst {
				if !d.Reg {
					delete(uses, d.Name)
				}
			}
		}
		s.stores = append(s.stores, &store{set: st, uses: uses, pure: pure})
		return true
	case *exec.Exit:
		if live {
			s.labels[st.Label] = true
		}
		return false
	case *exec.Break:
		if live {
			s.labels[st.Label] = true
		}
		return false
	case *exec.Continue:
		return false
	case *exec.Return:
		if live {
			for _, x := range st.Values {
				s.reads(x, s.hard)
			}
		}
		return false
	case *exec.Let:
		if live {
			s.reads(st.Value, s.hard)
		}
		return live
	case *exec.Do:
		if live {
			s.reads(st.Call, s.hard)
		}
		return live
	case *exec.Assert:
		if live {
			s.reads(st.Cond, s.hard)
		}
		return live
	}
	return live
}
