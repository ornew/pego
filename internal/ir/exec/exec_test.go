package exec

import (
	"strings"
	"testing"
)

// testModule declares a small parser VM: a counter component with two operations.
func testModule() *Module {
	m := NewModule("test", "budget")
	m.AddComponent("counter", "A counter.")
	m.AddRegister("pos", TInt, "The position.")
	tok := m.AddHandle("Token", "counter", "A token of the counter.")
	m.AddOp(&Op{Name: "counter.next", Component: "counter", Results: []Type{TInt, tok},
		Reads: []string{"counter"}, Writes: []string{"counter"},
		Contract: "Returns the count before the call and a token, and adds one.",
		Cost:     Cost{Const: 1}})
	m.AddOp(&Op{Name: "counter.charge", Component: "counter", Params: []Param{P("n", TU64)},
		Features: []string{"budget"}, NoReturn: true, Contract: "Charges n.",
		Cost: Cost{Terms: []CostTerm{{Coef: 1, Quantity: "n"}}}})
	return m
}

func TestBuildVerifyAndPrint(t *testing.T) {
	m := testModule()
	m.AddFunc("count", []Param{P("limit", TInt)}, []Type{TInt}, []string{"budget"}, func(b *Builder) {
		b.Let("sum", Int(0))
		b.Loop("each", func(b *Builder) {
			r := b.LetN([]string{"n", "tok"}, C("counter.next"))
			b.If(Bin(">=", r[0], L("limit")), func(b *Builder) { b.Break("each") })
			b.Set("sum", Add(L("sum"), r[0]))
			b.Do("counter.charge", U64(1))
			b.SetReg("pos", Add(R("pos"), Int(1)))
		})
		b.Return(L("sum"))
	})
	if err := Verify(m); err != nil {
		t.Fatal(err)
	}
	got := String(m)
	for _, want := range []string{
		"op counter.next() (int, Token)\n  component counter\n  reads counter\n  writes counter\n  cost <= 1\n",
		"  noreturn\n  cost <= 0 + 1*n\n",
		"func count(limit int) int [budget] {\n  let sum = 0\n  loop each {\n    let n, tok = counter.next()\n" +
			"    if (n >= limit) {\n      break each\n    }\n    sum = (sum + n)\n    counter.charge(u64(1))\n" +
			"    $pos = ($pos + 1)\n  }\n  return sum\n}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("listing lacks\n%s\nin\n%s", want, got)
		}
	}
}

func TestVerifyRejects(t *testing.T) {
	cases := []struct {
		name string
		edit func(m *Module)
		want string
	}{
		{"foreign write", func(m *Module) {
			m.AddComponent("other", "")
			m.AddOp(&Op{Name: "other.x", Component: "other", Writes: []string{"counter"}, Contract: "x"})
		}, "writes component counter, which is not its own"},
		{"no contract", func(m *Module) {
			m.AddOp(&Op{Name: "counter.y", Component: "counter"})
		}, "no contract"},
		{"undeclared handle", func(m *Module) {
			m.AddOp(&Op{Name: "counter.z", Component: "counter", Results: []Type{THandle("Nope")}, Contract: "z"})
		}, "undeclared handle type Nope"},
		{"feature missing", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Do("counter.charge", U64(1)) })
		}, "needs feature budget"},
		{"exit without block", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Exit("out") })
		}, `exit "out" names no enclosing block`},
		{"break of a block", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Block("out", func(b *Builder) { b.Break("out") }) })
		}, `break "out" names no enclosing loop`},
		{"read before let", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Assert(Lt(L("x"), Int(1)), "") })
		}, "read of undeclared local x"},
		{"local out of scope", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) {
				b.Block("", func(b *Builder) { b.Let("x", Int(1)) })
				b.Set("x", Int(2))
			})
		}, "set of undeclared local x"},
		{"type mismatch", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Let("x", Add(Int(1), U64(2))) })
		}, "operator + on int and u64"},
		{"arity", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Let("x", C("counter.next")) })
		}, "counter.next has 2 results where one value is needed"},
		{"return type", func(m *Module) {
			m.AddFunc("f", nil, []Type{TBool}, nil, func(b *Builder) { b.Return(Int(1)) })
		}, "return: int, want bool"},
		{"bit operator on int", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Let("x", Bin("&", Int(1), Int(2))) })
		}, "operator & on int"},
		{"missing immediate", func(m *Module) {
			m.AddOp(&Op{Name: "counter.lit", Component: "counter", Imm: "literal", Contract: "lit"})
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Do("counter.lit") })
		}, `immediate operand <nil>, want "literal"`},
		{"unexpected immediate", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.add(&Do{Call: CI("counter.next", "x")}) })
		}, `immediate operand x, want ""`},
		{"set arity", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) {
				b.Var("x", TInt)
				b.SetN([]Place{{Name: "x"}, {}, {}}, C("counter.next"))
			})
		}, "set of 3 places: counter.next has 2 results"},
		{"set type", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) {
				b.Var("x", TInt)
				b.Var("y", TInt)
				b.SetN([]Place{{Name: "x"}, {Name: "y"}}, C("counter.next"))
			})
		}, "set y: Token, want int"},
		{"null of a scalar", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) { b.Let("x", &Const{Type: TInt}) })
		}, "int constant holds <nil>"},
		{"let shadows a declared local", func(m *Module) {
			m.AddFunc("f", nil, nil, nil, func(b *Builder) {
				b.Var("x", TInt)
				b.Let("x", Int(1))
			})
		}, "local x shadows an enclosing local"},
		{"shadowing", func(m *Module) {
			m.AddFunc("f", []Param{P("x", TInt)}, nil, nil, func(b *Builder) {
				b.Block("", func(b *Builder) { b.Let("x", Int(1)) })
			})
		}, "local x shadows an enclosing local"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := testModule()
			c.edit(m)
			err := Verify(m)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Verify = %v, want an error containing %q", err, c.want)
			}
		})
	}
}
