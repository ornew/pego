package pass

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/ornew/zeroparse/internal/ir/exec"
)

// module declares a log component whose operations record calls, so that a test sees which
// operations a function runs and in what order.
func module() *Module {
	m := NewModule("test", "budget", "typed")
	m.AddComponent("log", "The calls made.")
	m.AddOp(&Op{Name: "log.note", Component: "log", Params: []Param{P("x", TInt)}, Writes: []string{"log"}, Contract: "Records x."})
	m.AddOp(&Op{Name: "log.charge", Component: "log", Features: []string{"budget"}, Writes: []string{"log"}, Contract: "Records a charge."})
	m.AddOp(&Op{Name: "log.flag", Component: "log", Results: []Type{TBool}, Writes: []string{"log"}, Contract: "Records a call and returns true."})
	return m
}

type logger struct{ calls []string }

func (l *logger) Call(o *Op, args []any) []any {
	switch o.Name {
	case "log.note":
		l.calls = append(l.calls, fmt.Sprint("note ", args[0]))
		return nil
	case "log.charge":
		l.calls = append(l.calls, "charge")
		return nil
	case "log.flag":
		l.calls = append(l.calls, "flag")
		return []any{true}
	}
	panic(o.Name)
}

func TestSpecialize(t *testing.T) {
	m := module()
	f := m.AddFunc("f", []Param{P("n", TInt)}, []Type{TInt}, []string{"budget", "typed"}, func(b *Builder) {
		b.Let("acc", Int(0))
		b.Loop("l", func(b *Builder) {
			b.If(Bin(">=", L("acc"), L("n")), func(b *Builder) { b.Break("l") })
			b.If(F("budget"), func(b *Builder) { b.Do("log.charge") })
			b.IfElse(And(F("typed"), C("log.flag")), func(b *Builder) {
				b.Set("acc", Add(L("acc"), Int(2)))
			}, func(b *Builder) {
				b.Set("acc", Add(L("acc"), Int(1)))
			})
			b.Do("log.note", L("acc"))
		})
		b.Block("out", func(b *Builder) {
			b.If(Not(F("typed")), func(b *Builder) { b.Exit("out") })
			b.Do("log.note", Int(-1))
		})
		b.Return(Add(L("acc"), Bin("*", Int(10), Int(3))))
	})
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	for _, on := range [][]string{nil, {"budget"}, {"typed"}, {"budget", "typed"}} {
		s, err := Specialize(m, f, "f_"+strings.Join(on, "_"), on)
		if err != nil {
			t.Fatal(err)
		}
		m.Funcs = append(m.Funcs, s)
		if err := Verify(m); err != nil {
			t.Fatalf("%v: %v\n%s", on, err, FuncString(s))
		}
		Walk(s.Body, nil, func(x Expr) {
			if _, ok := x.(*Feature); ok {
				t.Errorf("%v: a feature is left in\n%s", on, FuncString(s))
			}
		})
		// The original run with the features of the specialization behaves as the specialization.
		g := Clone(f)
		g.Features = on
		var want, got logger
		wr, err1 := Run(m, g, &want, int64(5))
		gr, err2 := Run(m, s, &got, int64(5))
		if err1 != nil || err2 != nil || wr[0] != gr[0] || strings.Join(want.calls, ",") != strings.Join(got.calls, ",") {
			t.Errorf("%v: original %v %v %v, specialized %v %v %v\n%s", on, wr, want.calls, err1, gr, got.calls, err2, FuncString(s))
		}
		listing := FuncString(s)
		if (len(on) == 0 || on[0] != "budget") && strings.Contains(listing, "log.charge") {
			t.Errorf("%v: the charge is left in\n%s", on, listing)
		}
		if !strings.Contains(listing, "(acc + 30)") {
			t.Errorf("%v: 10 * 3 is not folded\n%s", on, listing)
		}
	}
	if _, err := Specialize(m, f, "bad", []string{"nope"}); err == nil {
		t.Error("Specialize accepted an undeclared feature")
	}
}

func TestSimplifyKeepsEffects(t *testing.T) {
	m := module()
	f := m.AddFunc("f", nil, []Type{TBool, TInt}, nil, func(b *Builder) {
		// The call on the left of a constant false must still run.
		b.Let("x", And(C("log.flag"), BoolC(false)))
		b.Let("d", Int(0))
		b.Return(L("x"), Bin("/", Int(1), L("d")))
	})
	g := m.AddFunc("g", nil, []Type{TInt}, nil, func(b *Builder) {
		b.Return(Bin("/", Int(1), Int(0)))
	})
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	Simplify(f)
	Simplify(g)
	if !strings.Contains(FuncString(f), "log.flag()") {
		t.Errorf("the call was dropped\n%s", FuncString(f))
	}
	if !strings.Contains(FuncString(g), "(1 / 0)") {
		t.Errorf("a division by zero was folded\n%s", FuncString(g))
	}
}

func TestSimplifyPrunes(t *testing.T) {
	m := module()
	f := m.AddFunc("f", nil, nil, nil, func(b *Builder) {
		b.Block("a", func(b *Builder) { b.Do("log.note", Int(1)) })
		b.Block("b", func(b *Builder) {
			b.Exit("b")
			b.Do("log.note", Int(2))
		})
		b.For("l", nil, BoolC(false), nil, func(b *Builder) { b.Do("log.note", Int(3)) })
		b.Block("", func(b *Builder) {
			b.Let("y", Int(4))
			b.Do("log.note", L("y"))
		})
	})
	Simplify(f)
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	want := "func f() {\n  log.note(1)\n  block b {\n    exit b\n  }\n  {\n    let y = 4\n    log.note(y)\n  }\n}\n"
	if got := FuncString(f); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
