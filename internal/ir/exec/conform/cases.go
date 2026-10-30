package conform

import (
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/ops"
	"github.com/ornew/pego/internal/ir/exec/ref"
)

// noValue and noErrs are the value and the recovered errors that cases store in entries: the
// harness compares handles only by identity, and the model passes them through.
var noValue, noErrs = exec.Null(exec.THandle(ops.Value)), exec.Null(exec.THandle(ops.Errs))

// Cases returns the handwritten conformance cases of the pilot operations, in one module. Each case
// states the behavior it checks; the model checks it too, so a case that is wrong about the
// contract fails on the model before it judges an implementation.
func Cases() []Case {
	m := Module()
	var cs []Case
	add := func(name string, cfg ref.Config, body func(b *exec.Builder)) {
		cs = append(cs, Case{Module: m, Func: m.AddFunc(name, nil, nil, []string{"budget"}, body), Config: cfg})
	}
	vals := func(id int64) exec.Expr { return exec.C(ValuesOp, exec.Int(id)) }
	key := func(rule, pos, level, id int64) []exec.Expr {
		return []exec.Expr{exec.Int(rule), exec.Int(pos), exec.Int(level), vals(id)}
	}
	store := func(b *exec.Builder, name string, k []exec.Expr, ok bool, end int64) exec.Expr {
		return b.Let(name, exec.C("memo.store", append(slices.Clone(k), exec.BoolC(ok), exec.Int(end), exec.BoolC(!ok), noValue, noErrs)...))
	}
	lookup := func(b *exec.Builder, e, found string, k []exec.Expr) (exec.Expr, exec.Expr) {
		r := b.LetN([]string{e, found}, exec.C("memo.lookup", k...))
		return r[0], r[1]
	}

	add("memo_store_lookup", ref.Config{}, func(b *exec.Builder) {
		k := key(1, 5, 0, 7)
		_, found := lookup(b, "none", "f0", k)
		b.Assert(exec.Not(found), "an empty memo has no entry")
		e := store(b, "e", k, true, 9)
		got, found := lookup(b, "got", "f1", k)
		b.Assert(exec.And(found, exec.Eq(got, e)), "lookup returns the stored entry")
		b.Assert(exec.And(exec.C("memo.ok", got), exec.Eq(exec.C("memo.end", got), exec.Int(9))), "the entry keeps its outcome and end")
		b.Assert(exec.Not(exec.C("memo.growing", got)), "a stored entry is complete")
		b.Assert(exec.Not(exec.C("memo.silent", got)), "the entry keeps whether it is silent")
		for i, other := range [][]exec.Expr{key(1, 5, 0, 8), key(2, 5, 0, 7), key(1, 5, 1, 7), key(1, 6, 0, 7)} {
			_, f := lookup(b, fmt.Sprint("o", i), fmt.Sprint("of", i), other)
			b.Assert(exec.Not(f), "keys differing in values, rule, level or position are distinct")
		}
		e2 := store(b, "e2", k, false, 5)
		got2, _ := lookup(b, "got2", "f2", k)
		b.Assert(exec.And(exec.Eq(got2, e2), exec.Not(exec.C("memo.ok", got2))), "a store replaces the entry under the same key")
		b.Assert(exec.C("memo.silent", got2), "with its silent attribute")
	})

	add("memo_growth", ref.Config{}, func(b *exec.Builder) {
		k := key(3, 2, 0, 1)
		s := b.Let("s", exec.C("memo.seed", k...))
		b.Assert(exec.And(exec.C("memo.growing", s), exec.Not(exec.C("memo.ok", s))), "a seed is a growing failure")
		b.Assert(exec.Eq(exec.C("memo.end", s), exec.Int(2)), "a seed ends at its position")
		g, found := b.LetN([]string{"g", "fg"}, exec.C("memo.lookupGrowing", exec.Int(3), exec.Int(2), exec.Int(0)))[0], exec.L("fg")
		b.Assert(exec.And(found, exec.Eq(g, s)), "the guard finds the seed")
		s2 := b.Let("s2", exec.C("memo.reseed", append(slices.Clone(k), exec.Int(6), noValue, noErrs)...))
		g2 := b.LetN([]string{"g2", "fg2"}, exec.C("memo.lookupGrowing", exec.Int(3), exec.Int(2), exec.Int(0)))[0]
		b.Assert(exec.Eq(g2, s2), "the guard finds the longer seed")
		got, _ := lookup(b, "got", "f", k)
		b.Assert(exec.Eq(got, s2), "lookup of the seed's key finds the longer seed")
		b.Do("memo.finishSeed", s2, exec.BoolC(true))
		_, fg3 := b.LetN([]string{"g3", "fg3"}, exec.C("memo.lookupGrowing", exec.Int(3), exec.Int(2), exec.Int(0)))[0], exec.L("fg3")
		b.Assert(exec.Not(fg3), "a finished growth has no growing entry")
		done, _ := lookup(b, "done", "fd", k)
		b.Assert(exec.And(exec.Eq(done, s2), exec.And(exec.C("memo.ok", done), exec.Not(exec.C("memo.growing", done)))), "the finished seed is a complete success")
		b.Assert(exec.Eq(exec.C("memo.end", done), exec.Int(6)), "with the end of the last growth")
		b.Assert(exec.C("memo.silent", done), "and the silent attribute given when it finished")
	})

	add("memo_guard_ignores_values", ref.Config{}, func(b *exec.Builder) {
		a := store(b, "a", key(4, 0, 0, 1), true, 1)
		s := b.Let("s", exec.C("memo.seed", key(4, 0, 0, 2)...))
		g := b.LetN([]string{"g", "fg"}, exec.C("memo.lookupGrowing", exec.Int(4), exec.Int(0), exec.Int(0)))[0]
		b.Assert(exec.Eq(g, s), "the guard finds the seed, not the entry under other values")
		b.Do("memo.finishSeed", s, exec.BoolC(false))
		got, found := lookup(b, "got", "f", key(4, 0, 0, 1))
		b.Assert(exec.And(found, exec.Eq(got, a)), "the entry under other values survives the growth")
	})

	add("memo_many_positions", ref.Config{}, func(b *exec.Builder) {
		b.Let("pos", exec.Int(0))
		b.Loop("fill", func(b *exec.Builder) {
			b.If(exec.Bin(">=", exec.L("pos"), exec.Int(300)), func(b *exec.Builder) { b.Break("fill") })
			for rule := int64(0); rule < 3; rule++ {
				b.Do("memo.store", exec.Int(rule), exec.L("pos"), exec.Int(0), vals(0), exec.BoolC(true), exec.Add(exec.L("pos"), exec.Int(rule)),
					exec.BoolC(false), noValue, noErrs)
			}
			b.Set("pos", exec.Add(exec.L("pos"), exec.Int(7)))
		})
		b.Set("pos", exec.Int(0))
		b.Loop("check", func(b *exec.Builder) {
			b.If(exec.Bin(">=", exec.L("pos"), exec.Int(300)), func(b *exec.Builder) { b.Break("check") })
			r := b.LetN([]string{"e", "f"}, exec.C("memo.lookup", exec.Int(2), exec.L("pos"), exec.Int(0), vals(0)))
			b.Assert(exec.And(r[1], exec.Eq(exec.C("memo.end", r[0]), exec.Add(exec.L("pos"), exec.Int(2)))), "every stored position keeps its entry")
			b.Set("pos", exec.Add(exec.L("pos"), exec.Int(7)))
		})
	})

	add("seen_first_and_repeat", ref.Config{Rules: 3}, func(b *exec.Builder) {
		b.Assert(exec.C("seen.first", exec.Int(0), exec.Int(10)), "the first call at a position is deferred")
		b.Assert(exec.C("seen.first", exec.Int(1), exec.Int(10)), "rules are recorded separately")
		b.Assert(exec.C("seen.first", exec.Int(0), exec.Int(11)), "positions are recorded separately")
		b.Assert(exec.Not(exec.C("seen.first", exec.Int(0), exec.Int(10))), "a repeated call is memoized")
	})

	add("seen_becomes_eager", ref.Config{Rules: 2}, func(b *exec.Builder) {
		// 40 first calls, then repeats until more than calls/16 repeated: the rule turns eager.
		b.Let("pos", exec.Int(0))
		b.Loop("firsts", func(b *exec.Builder) {
			b.If(exec.Bin(">=", exec.L("pos"), exec.Int(40)), func(b *exec.Builder) { b.Break("firsts") })
			b.Assert(exec.C("seen.first", exec.Int(1), exec.L("pos")), "first calls are deferred")
			b.Set("pos", exec.Add(exec.L("pos"), exec.Int(1)))
		})
		b.Assert(exec.Not(exec.C("seen.first", exec.Int(1), exec.Int(0))), "repeat 1")
		b.Assert(exec.Not(exec.C("seen.first", exec.Int(1), exec.Int(1))), "repeat 2")
		b.Assert(exec.Not(exec.C("seen.first", exec.Int(1), exec.Int(2))), "repeat 3: 3 > 43/16, eager")
		b.Assert(exec.Not(exec.C("seen.first", exec.Int(1), exec.Int(100))), "an eager rule memoizes a first call")
		b.Assert(exec.C("seen.first", exec.Int(0), exec.Int(100)), "another rule is not eager")
	})

	add("seen_regions", ref.Config{Rules: 130}, func(b *exec.Builder) {
		for _, pos := range []int64{0, 63, 64, 1023, 1024, 1025, 5000, 65535} {
			for _, rule := range []int64{0, 64, 129} {
				b.Assert(exec.C("seen.first", exec.Int(rule), exec.Int(pos)), "first call in a region")
			}
		}
		b.Assert(exec.Not(exec.C("seen.first", exec.Int(129), exec.Int(1023))), "returning to an earlier region keeps its record")
	})

	add("budget_limit", ref.Config{Limit: 10}, func(b *exec.Builder) {
		for i := 0; i < 3; i++ {
			b.Do("budget.charge", exec.U64(3))
		}
		b.Assert(exec.Eq(exec.C("budget.used"), exec.U64(9)), "charges add up")
		b.Do("budget.charge", exec.U64(1))
		b.Assert(exec.Eq(exec.C("budget.used"), exec.U64(10)), "the limit itself is allowed")
		b.Do("budget.charge", exec.U64(1)) // ends the parse
		b.Assert(exec.BoolC(false), "unreachable: the charge beyond the limit interrupts")
	})

	add("budget_unlimited", ref.Config{}, func(b *exec.Builder) {
		b.Do("budget.charge", exec.U64(1<<40))
		b.Do("budget.charge", exec.U64(0))
		b.Assert(exec.Eq(exec.C("budget.used"), exec.U64(1<<40)), "no limit: every charge is added")
	})

	add("depth", ref.Config{MaxDepth: 2}, func(b *exec.Builder) {
		b.Do("depth.enter")
		b.Do("depth.enter")
		b.Do("depth.exit")
		b.Assert(exec.Eq(exec.C("depth.current"), exec.Int(1)), "enter and exit balance")
		b.Do("depth.enter")
		b.Do("depth.enter") // ends the parse
		b.Assert(exec.BoolC(false), "unreachable: the depth beyond the limit interrupts")
	})
	return cs
}

// Random returns a randomized case of n operation calls from ops, drawn with rng, that meets every
// precondition. Arguments come from small domains, so that keys, positions and rules collide often,
// and from a few far positions, so that position-indexed structures grow and span regions.
func Random(name string, rng *rand.Rand, opNames []string, n int, cfg ref.Config) Case {
	m := Module()
	mirror := ref.New(cfg)
	type entry struct {
		name string
		ref  *ref.Entry
	}
	var entries []entry
	f := m.AddFunc(name, nil, nil, []string{"budget"}, func(b *exec.Builder) {
		pos := func() int64 {
			if rng.IntN(8) == 0 {
				return []int64{1023, 1024, 4096, 70000}[rng.IntN(4)]
			}
			return rng.Int64N(12)
		}
		calls := 0
		for i := 0; calls < n && i < 50*n; i++ {
			o := m.Op(opNames[rng.IntN(len(opNames))])
			var args []any
			var exprs []exec.Expr
			intArg := func(v int64) { args = append(args, v); exprs = append(exprs, exec.Int(v)) }
			keyArgs := func(withVals bool) {
				intArg(rng.Int64N(3))
				intArg(pos())
				intArg(rng.Int64N(2))
				if withVals {
					id := rng.Int64N(3)
					args = append(args, id)
					exprs = append(exprs, exec.C(ValuesOp, exec.Int(id)))
				}
			}
			switch o.Name {
			case "memo.lookup", "memo.seed":
				keyArgs(true)
			case "memo.lookupGrowing":
				keyArgs(false)
			case "memo.store":
				keyArgs(true)
				ok := rng.IntN(2) == 0
				args, exprs = append(args, ok), append(exprs, exec.BoolC(ok))
				intArg(args[1].(int64) + rng.Int64N(4))
				silent := rng.IntN(2) == 0
				args, exprs = append(args, silent, nil, nil), append(exprs, exec.BoolC(silent), noValue, noErrs)
			case "memo.reseed":
				keyArgs(true)
				intArg(args[1].(int64) + 1 + rng.Int64N(4))
				args, exprs = append(args, nil, nil), append(exprs, noValue, noErrs)
			case "memo.finishSeed", "memo.ok", "memo.end", "memo.growing", "memo.silent":
				if len(entries) == 0 {
					continue
				}
				e := entries[rng.IntN(len(entries))]
				args, exprs = append(args, e.ref), append(exprs, exec.L(e.name))
				if o.Name == "memo.finishSeed" {
					silent := rng.IntN(2) == 0
					args, exprs = append(args, silent), append(exprs, exec.BoolC(silent))
				}
			case "seen.first":
				intArg(rng.Int64N(int64(max(cfg.Rules, 1))))
				intArg(pos())
			case "budget.charge":
				v := rng.Uint64N(8)
				args, exprs = append(args, v), append(exprs, exec.U64(v))
			}
			if !mirror.Allows(o, args) {
				continue
			}
			res, stop := mirrorCall(mirror, o, args)
			calls++
			switch {
			case len(o.Results) > 0 && o.Results[0].Kind == exec.KHandle:
				name := fmt.Sprintf("e%d", calls)
				if len(o.Results) == 2 {
					b.LetN([]string{name, name + "f"}, exec.C(o.Name, exprs...))
					if !res[1].(bool) {
						continue // an entry that was not found must not be used
					}
				} else {
					b.Let(name, exec.C(o.Name, exprs...))
				}
				entries = append(entries, entry{name, res[0].(*ref.Entry)})
			default:
				b.Do(o.Name, exprs...)
			}
			if stop {
				return
			}
		}
	})
	return Case{Module: m, Func: f, Config: cfg}
}

// mirrorCall runs a call on the generator's model and reports whether it ended the parse.
func mirrorCall(m *ref.Model, o *exec.Op, args []any) (res []any, stop bool) {
	defer func() {
		if x := recover(); x != nil {
			if _, ok := x.(exec.Interrupt); !ok {
				panic(x)
			}
			stop = true
		}
	}()
	return m.Call(o, args), false
}
