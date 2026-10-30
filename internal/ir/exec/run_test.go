package exec

import (
	"errors"
	"testing"
)

// counter is a Machine for testModule: counter.next counts, counter.charge interrupts beyond 3.
type counter struct {
	n, charged int64
}

type token struct{ n int64 }

func (c *counter) Call(o *Op, args []any) []any {
	switch o.Name {
	case "counter.next":
		c.n++
		return []any{c.n - 1, &token{c.n - 1}}
	case "counter.charge":
		c.charged += int64(args[0].(uint64))
		if c.charged > 3 {
			panic(Interrupt{Reason: "budget"})
		}
		return nil
	}
	panic("unknown op " + o.Name)
}

func TestRun(t *testing.T) {
	m := testModule()
	sum := m.AddFunc("sum", []Param{P("limit", TInt)}, []Type{TInt, TInt}, nil, func(b *Builder) {
		b.Let("s", Int(0))
		b.Loop("each", func(b *Builder) {
			r := b.LetN([]string{"n", "tok"}, C("counter.next"))
			b.If(Eq(Bin("%", r[0], Int(2)), Int(1)), func(b *Builder) { b.Continue("each") })
			b.If(Bin(">=", r[0], L("limit")), func(b *Builder) { b.Break("each") })
			b.Set("s", Add(L("s"), r[0]))
		})
		b.Block("out", func(b *Builder) {
			b.Exit("out")
			b.Set("s", Int(-1)) // unreachable
		})
		b.SetReg("pos", Add(R("pos"), Int(7)))
		b.Return(L("s"), R("pos"))
	})
	wrap := m.AddFunc("wrap", nil, []Type{TU32, TInt}, nil, func(b *Builder) {
		b.Let("u", U32(0xffffffff))
		b.Set("u", Bin("+", L("u"), U32(2)))
		b.Return(L("u"), Conv(TInt, Conv(TI32, Int(1<<31))))
	})
	charge := m.AddFunc("charge", nil, nil, []string{"budget"}, func(b *Builder) {
		b.Loop("l", func(b *Builder) { b.Do("counter.charge", U64(2)) })
	})
	fails := m.AddFunc("fails", nil, nil, nil, func(b *Builder) {
		b.LetN([]string{"x", "p"}, C("sum", Int(3)))
		b.Assert(Eq(L("x"), Int(99)), "x is 99")
	})
	// Locals at entry, loop clauses (Post also after a Continue), an If with Init, an assignment of
	// several places from one call (with a register and a discarded result) and the null handle.
	clauses := m.AddFunc("clauses", nil, []Type{TInt, TInt, TBool}, nil, func(b *Builder) {
		i, s := b.Var("i", TInt), b.Var("s", TInt)
		tok := b.Var("tok", THandle("Token"))
		b.Var("null", TBool)
		b.Set("null", Eq(tok, Null(THandle("Token"))))
		b.For("l", Assign("i", Int(0)), Lt(i, Int(5)), Assign("i", Add(i, Int(1))), func(b *Builder) {
			b.If(Eq(i, Int(2)), func(b *Builder) { b.Continue("l") })
			b.Set("s", Add(s, i))
		})
		b.SetN([]Place{{Name: "pos", Reg: true}, {}}, C("counter.next"))
		b.add(&If{Init: Assign("s", Add(s, Int(100))), Cond: Lt(s, Int(0)), Then: &Block{}})
		b.Comment("does nothing")
		b.Return(Add(s, i), R("pos"), L("null"))
	})
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	if res, err := Run(m, clauses, &counter{n: 4}); err != nil || res[0] != int64(0+1+3+4+100+5) || res[1] != int64(4) || res[2] != true {
		t.Fatalf("clauses = %v, %v", res, err)
	}
	res, err := Run(m, sum, &counter{}, int64(6))
	if err != nil || res[0] != int64(0+2+4) || res[1] != int64(7) {
		t.Fatalf("sum = %v, %v", res, err)
	}
	res, err = Run(m, wrap, &counter{})
	if err != nil || res[0] != uint32(1) || res[1] != int64(-1<<31) {
		t.Fatalf("wrap = %v, %v", res, err)
	}
	if _, err = Run(m, charge, &counter{}); !errors.As(err, new(Interrupt)) {
		t.Fatalf("charge = %v, want an interrupt", err)
	}
	var ae *AssertionError
	if _, err = Run(m, fails, &counter{}); !errors.As(err, &ae) || ae.Msg != "x is 99" {
		t.Fatalf("fails = %v, want the assertion", err)
	}
}
