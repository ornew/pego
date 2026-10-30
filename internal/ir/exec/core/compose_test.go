package core

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/ops"
	"github.com/ornew/pego/internal/ir/exec/pass"
	"github.com/ornew/pego/internal/ir/exec/ref"
)

// The tests of this file run the compositions on a model of the operations: the reference model of
// the pilot operations (ref.Model) and a model of the transitional ones written from their
// contracts, with rules whose bodies the tests script. They check the contracts of the compositions
// (what a call leaves in the parser state, what it charges, which entries it reuses) without a
// generated parser, so that they hold in a run without the Exec IR switch, and where a generated
// parser cannot tell (it restores the position after a failed call itself).

// trule is a rule of the model: its flags and its scripted body, which runs at the position of the
// machine and returns the value and success of the body.
type trule struct {
	id                   int64
	memo, leader, defer_ bool
	body                 func(m *machine) (any, bool)
	runs                 int // the evaluations of the body
}

// record is an expectation record of the model: the farthest position and the expectations there.
type record struct {
	far  int64
	exps []string
}

// machine models the operations of the compositions.
type machine struct {
	*ref.Model
	regs map[string]any
	rec  []string
	// cur is the expectation record; saved holds the records that isolations set aside.
	cur   record
	saved []record
	// exps holds the expectations of each entry (memo.setExpected).
	exps map[*ref.Entry]record
	// pruned makes memo.grown find no entry, as after a stream commit.
	pruned                 bool
	spends, steps, memoHit int
}

func newMachine() *machine {
	return &machine{Model: ref.New(ref.Config{Rules: 4, MaxDepth: 100}), exps: map[*ref.Entry]record{}}
}

func (m *machine) UseRegisters(regs map[string]any) { m.regs = regs }

func (m *machine) pos() int64         { return m.regs[ops.Pos].(int64) }
func (m *machine) setPos(p int64)     { m.regs[ops.Pos] = p }
func (m *machine) silent() bool       { return m.regs[ops.Silent].(int64) > 0 }
func (m *machine) recover(e string)   { m.rec = append(m.rec, e) }
func (m *machine) expect(e string)    { m.merge(m.pos(), []string{e}) }
func (m *machine) advance(n int64)    { m.setPos(m.pos() + n) }
func (m *machine) record() []string   { return slices.Clone(m.rec) }
func (m *machine) farthest() int64    { return m.cur.far }
func (m *machine) expected() []string { return m.cur.exps }

// merge adds the expectations at far to the record, as the runtime's Expect does: outside lookaheads,
// at the farthest position or past it.
func (m *machine) merge(far int64, exps []string) {
	for _, e := range exps {
		if m.silent() || far < m.cur.far {
			continue
		}
		if far > m.cur.far {
			m.cur = record{far: far}
		}
		if !slices.Contains(m.cur.exps, e) {
			m.cur.exps = append(m.cur.exps, e)
		}
	}
}

func entryOf(x any) *ref.Entry {
	e, _ := x.(*ref.Entry)
	if e == nil {
		panic(exec.Interrupt{Reason: "the null entry is used"})
	}
	return e
}

func (m *machine) Call(o *exec.Op, args []any) []any {
	switch o.Name {
	case "call.tracked":
		return []any{false}
	case "call.spend":
		m.spends++
		return nil
	case "call.growStep":
		m.steps++
		return nil
	case "call.first":
		r := args[0].(*trule)
		if !r.defer_ {
			return []any{false}
		}
		return m.Model.Call(&exec.Op{Name: "seen.first"}, []any{r.id, m.pos()})
	case "rule.id":
		return []any{args[0].(*trule).id}
	case "rule.memo":
		return []any{args[0].(*trule).memo}
	case "rule.leader":
		return []any{args[0].(*trule).leader}
	case "rule.plain":
		return []any{false}
	case "rule.invoke":
		r := args[0].(*trule)
		r.runs++
		v, ok := r.body(m)
		return []any{v, ok}
	case "env.memoKey":
		return []any{int64(0)}
	case "rec.mark":
		return []any{int64(len(m.rec))}
	case "rec.truncate":
		m.rec = m.rec[:args[0].(int64)]
		return nil
	case "rec.since":
		return []any{slices.Clone(m.rec[args[0].(int64):])}
	case "rec.add":
		errs, _ := args[0].([]string)
		m.rec = append(m.rec, errs...)
		return nil
	case "rec.none":
		errs, _ := args[0].([]string)
		return []any{len(errs) == 0}
	case "exp.isolate":
		m.saved = append(m.saved, m.cur)
		m.cur = record{far: args[0].(int64)}
		return []any{len(m.saved)}
	case "exp.unisolate":
		if args[0].(int) != len(m.saved) {
			panic("exp.unisolate of a record that is not the last set aside")
		}
		ended := m.cur
		m.cur, m.saved = m.saved[len(m.saved)-1], m.saved[:len(m.saved)-1]
		return []any{ended.far, ended.exps}
	case "exp.keep":
		s, _ := args[0].([]string)
		return []any{slices.Clone(s)}
	case "exp.merge", "exp.mergeKept":
		s, _ := args[1].([]string)
		m.merge(args[0].(int64), s)
		return nil
	case "memo.far":
		return []any{m.exps[entryOf(args[0])].far}
	case "memo.expected":
		return []any{m.exps[entryOf(args[0])].exps}
	case "memo.setExpected":
		s, _ := args[2].([]string)
		m.exps[entryOf(args[0])] = record{far: args[1].(int64), exps: s}
		return nil
	case "memo.grown":
		res := m.Model.Call(&exec.Op{Name: "memo.lookup"}, args)
		if m.pruned || !res[1].(bool) {
			return []any{nil}
		}
		return []any{res[0]}
	case "memo.detached":
		return []any{&ref.Entry{}}
	case "obs.memoHit":
		m.memoHit++
		return nil
	}
	return m.Model.Call(o, args)
}

// composition returns the composition fn of the module m specialized for on.
func composition(t *testing.T, m *exec.Module, fn string, on ...string) *exec.Func {
	t.Helper()
	f, err := pass.Specialize(m, m.Func(fn), fn, on)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// callAt calls the rule r at the position pos with the composition call specialized for on, and
// returns its value and success.
func callAt(t *testing.T, mod *exec.Module, m *machine, f *exec.Func, r *trule, pos int64) (any, bool) {
	t.Helper()
	// Run starts with zero registers; the machine keeps the position and depth between calls.
	silent := int64(0)
	if m.regs != nil {
		silent = m.regs[ops.Silent].(int64)
	}
	res, err := exec.Run(mod, f, &start{machine: m, pos: pos, silent: silent}, r, int64(0))
	if err != nil {
		t.Fatal(err)
	}
	return res[0], res[1].(bool)
}

// start sets the registers of a run before the function runs.
type start struct {
	*machine
	pos, silent int64
}

func (s *start) UseRegisters(regs map[string]any) {
	regs[ops.Pos], regs[ops.Silent] = s.pos, s.silent
	s.machine.UseRegisters(regs)
}

// TestCallFailureRestoresState checks that a failed call, unmemoized and through the memo, leaves the
// position where it started and drops the errors its body recovered.
func TestCallFailureRestoresState(t *testing.T) {
	mod := Build()
	f := composition(t, mod, "call")
	m := newMachine()
	r := &trule{id: 1, memo: true, defer_: true, body: func(m *machine) (any, bool) {
		m.recover("skipped")
		m.advance(2)
		return nil, false
	}}
	for i, path := range []string{"unmemoized", "memoized", "reused"} {
		if _, ok := callAt(t, mod, m, f, r, 3); ok {
			t.Fatalf("%s call succeeded", path)
		}
		if m.pos() != 3 || len(m.rec) != 0 {
			t.Errorf("%s call: position %d, recovered %v; want 3 and none", path, m.pos(), m.rec)
		}
		if want := min(i+1, 2); r.runs != want {
			t.Errorf("%s call: %d evaluations of the body, want %d", path, r.runs, want)
		}
	}
	if m.spends != 3 {
		t.Errorf("%d charges for 3 calls", m.spends)
	}
}

// TestCallSuccessKeepsErrors checks that a successful memoized call keeps the errors its body
// recovered and the expectations it recorded, and that the call that reuses its entry records them
// again and moves to its end.
func TestCallSuccessKeepsErrors(t *testing.T) {
	mod := Build()
	f := composition(t, mod, "call")
	m := newMachine()
	r := &trule{id: 1, memo: true, defer_: true, body: func(m *machine) (any, bool) {
		m.recover("skipped")
		m.advance(2)
		m.expect("x")
		return "v", true
	}}
	for i := range 3 {
		m.rec, m.cur = nil, record{}
		v, ok := callAt(t, mod, m, f, r, 3)
		if !ok || v != "v" || m.pos() != 5 || !slices.Equal(m.rec, []string{"skipped"}) {
			t.Errorf("call %d: %v %v at %d, recovered %v; want v true at 5 and one error", i, v, ok, m.pos(), m.rec)
		}
		if m.farthest() != 5 || !slices.Equal(m.expected(), []string{"x"}) {
			t.Errorf("call %d: the record holds %d %v, want 5 [x]", i, m.farthest(), m.expected())
		}
	}
	if r.runs != 2 {
		t.Errorf("%d evaluations of the body, want 2", r.runs)
	}
}

// TestCallExpectationsFromStart checks that a memoized call records its expectations in a record
// that starts at its position: its entry keeps that position as the farthest one when the body
// recorded nothing past it, and the expectations it recorded otherwise.
func TestCallExpectationsFromStart(t *testing.T) {
	mod := Build()
	f := composition(t, mod, "call")
	for _, c := range []struct {
		name string
		exp  bool
	}{{"nothing recorded", false}, {"an expectation", true}} {
		t.Run(c.name, func(t *testing.T) {
			m := newMachine()
			r := &trule{id: 1, memo: true, body: func(m *machine) (any, bool) {
				if c.exp {
					m.advance(1)
					m.expect("x")
				}
				return nil, false
			}}
			callAt(t, mod, m, f, r, 4)
			res := m.Model.Call(&exec.Op{Name: "memo.lookup"}, []any{int64(1), int64(4), int64(0), int64(0)})
			e := res[0].(*ref.Entry)
			want := record{far: 4}
			if c.exp {
				want = record{far: 5, exps: []string{"x"}}
			}
			if got := m.exps[e]; got.far != want.far || !slices.Equal(got.exps, want.exps) {
				t.Errorf("the entry keeps %+v, want %+v", got, want)
			}
			// An empty set of expectations leaves the enclosing record as it is.
			outer := record{}
			if c.exp {
				outer = want
			}
			if m.farthest() != outer.far || !slices.Equal(m.expected(), outer.exps) {
				t.Errorf("the record holds %d %v, want %+v", m.farthest(), m.expected(), outer)
			}
		})
	}
}

// TestCallReuseContexts checks which entries a call reuses: one computed in a lookahead only in
// one, and one with recovered errors only in the kind of context it was computed in.
func TestCallReuseContexts(t *testing.T) {
	mod := Build()
	f := composition(t, mod, "call")
	for _, c := range []struct {
		name                string
		errs                bool
		storeSilent, silent int64
		reused              bool
	}{
		{"plain entry in a lookahead", false, 0, 1, true},
		{"silent entry outside", false, 1, 0, false},
		{"silent entry in a lookahead", false, 1, 1, true},
		{"entry with errors in a lookahead", true, 0, 1, false},
		{"silent entry with errors in a lookahead", true, 1, 1, true},
		{"entry with errors outside", true, 0, 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newMachine()
			r := &trule{id: 1, memo: true, body: func(m *machine) (any, bool) {
				if c.errs {
					m.recover("skipped")
				}
				m.advance(1)
				return "v", true
			}}
			m.regs = map[string]any{ops.Silent: c.storeSilent}
			callAt(t, mod, m, f, r, 0)
			m.regs[ops.Silent] = c.silent
			callAt(t, mod, m, f, r, 0)
			if reused := r.runs == 1; reused != c.reused {
				t.Errorf("reused %v, want %v", reused, c.reused)
			}
		})
	}
}

// TestGrowth checks that a leader's call evaluates its body while the match gets longer, charging
// one growth step per evaluation, and ends at the longest match with its errors only.
func TestGrowth(t *testing.T) {
	mod := Build()
	for _, on := range [][]string{nil, {ops.Stream}} {
		for _, pruned := range []bool{false, true} {
			if pruned && on == nil {
				continue // outside streams a growth leaves its entry
			}
			t.Run(fmt.Sprintf("%v pruned=%v", on, pruned), func(t *testing.T) {
				f := composition(t, mod, "call", on...)
				m := newMachine()
				m.pruned = pruned
				lens := []int64{1, 3, 3}
				n := 0
				r := &trule{id: 2, memo: true, leader: true, body: func(m *machine) (any, bool) {
					if n >= len(lens) {
						return nil, false
					}
					m.recover(fmt.Sprint("e", n))
					m.advance(lens[n])
					n++
					return fmt.Sprint("v", n), true
				}}
				v, ok := callAt(t, mod, m, f, r, 2)
				if !ok || v != "v2" || m.pos() != 5 {
					t.Errorf("%v %v at %d, want v2 true at 5", v, ok, m.pos())
				}
				if !slices.Equal(m.rec, []string{"e1"}) {
					t.Errorf("recovered %v, want the errors of the longest match", m.rec)
				}
				if r.runs != 3 || m.steps != 3 {
					t.Errorf("%d evaluations and %d growth steps, want 3 and 3", r.runs, m.steps)
				}
			})
		}
	}
}
