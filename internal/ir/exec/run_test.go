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
	if err := Verify(m); err != nil {
		t.Fatal(err)
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
