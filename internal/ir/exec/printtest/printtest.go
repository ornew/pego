// Package printtest holds the printer tests of the Exec IR backends (design 065): small functions,
// one or more per construct, that a backend prints and runs, and whose output must equal what the
// interpreter (exec.Run) gives for them.
//
// The functions run against a machine with a log: log.note appends a line, log.flag appends one and
// returns its argument, and box.new and box.get make and read a handle. A backend's test implements
// these four operations in its language, prints each function of Funcs, runs every Entry and
// compares what it prints, line by line, with Want.
package printtest

import (
	"fmt"
	"math"
	"strings"

	. "github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/pass"
)

// Box is the handle type of the test module.
const Box = "Box"

// Build returns the test module with its functions. Every function without parameters is an
// entry (Entries).
func Build() *Module {
	m := NewModule("printtest", "extra")
	m.AddComponent("log", "The lines a run prints.")
	m.AddRegister("pos", TInt, "A register.")
	box := m.AddHandle(Box, "log", "A box holding an int.")
	m.AddOp(&Op{Name: "log.note", Component: "log", Params: []Param{P("x", TInt)}, Writes: []string{"log"}, Contract: "Prints x."})
	m.AddOp(&Op{Name: "log.flag", Component: "log", Params: []Param{P("b", TBool)}, Results: []Type{TBool}, Writes: []string{"log"},
		Contract: "Prints b and returns it."})
	m.AddOp(&Op{Name: "box.new", Component: "log", Params: []Param{P("x", TInt)}, Results: []Type{box}, Contract: "Returns a new box holding x."})
	m.AddOp(&Op{Name: "box.get", Component: "log", Params: []Param{P("b", box)}, Results: []Type{TInt}, Contract: "Returns what b holds."})

	note := func(b *Builder, x Expr) { b.Do("log.note", x) }

	// Integer wraparound, truncating division and conversions, on values that are not constants,
	// and constant expressions, which a backend folds as Exec IR evaluates them.
	m.AddFunc("wrap", nil, []Type{TI32, TU32, TU64, TI32, TU32, TInt, TU32, TInt, TInt, TInt, TI32, TU64}, nil, func(b *Builder) {
		b.Let("i", I32(math.MaxInt32))
		b.Let("u", U32(0))
		b.Let("w", U64(math.MaxUint64))
		b.Let("n", Int(-7))
		b.Let("lo", I32(math.MinInt32))
		b.Return(
			Bin("+", L("i"), I32(1)),
			Bin("-", L("u"), U32(1)),
			Bin("*", L("w"), U64(2)),
			Bin("<<", Bin("-", L("i"), I32(math.MaxInt32-1)), I32(31)),
			Bin(">>", Bin("-", L("u"), U32(1)), U32(1)),
			Conv(TInt, Conv(TI32, Add(Conv(TInt, L("i")), Int(1)))),
			Conv(TU32, Bin("-", L("lo"), Bin("-", L("lo"), I32(-1)))),
			Conv(TInt, Conv(TU64, L("n"))),
			Bin("/", L("n"), Int(2)),
			Bin("%", L("n"), Int(2)),
			Bin("/", L("lo"), I32(-1)),
			Bin("^", Bin("|", Bin("&", L("w"), U64(0xf0)), U64(0x0f)), Bin("&", Bin("&", L("w"), U64(0xff)), U64(0x3c))),
		)
	})
	m.AddFunc("constants", nil, []Type{TI32, TU32, TU64, TInt, TU32, TInt, TBool}, nil, func(b *Builder) {
		b.Return(
			Bin("+", I32(math.MaxInt32), I32(1)),
			Bin("-", U32(0), U32(1)),
			Bin("*", U64(math.MaxUint64), U64(2)),
			Conv(TInt, Conv(TI32, Int(1<<31))),
			Conv(TU32, I32(-1)),
			Conv(TInt, Conv(TU64, Int(-1))),
			Not(Bin("<", Bin("<<", I32(1), I32(31)), I32(0))),
		)
	})
	// Wraparound of locals in a loop: a u32 hash and an i32 counter.
	m.AddFunc("hash", nil, []Type{TU32, TI32}, nil, func(b *Builder) {
		b.Let("h", U32(2166136261))
		b.Let("n", I32(math.MaxInt32-2))
		b.For("l", nil, nil, nil, func(b *Builder) {
			b.If(Lt(L("n"), I32(0)), func(b *Builder) { b.Break("l") })
			b.Set("h", Bin("*", Bin("^", L("h"), Conv(TU32, L("n"))), U32(16777619)))
			b.Set("n", Bin("+", L("n"), I32(1)))
		})
		b.Return(L("h"), L("n"))
	})
	// Labeled exits: out of nested blocks, out of loops, a break of an outer loop and a continue of
	// an outer loop from an inner one, and two sibling blocks with one label.
	m.AddFunc("exits", nil, []Type{TInt}, nil, func(b *Builder) {
		b.Let("i", Int(0))
		b.Block("out", func(b *Builder) {
			b.Block("in", func(b *Builder) {
				b.If(Eq(L("i"), Int(0)), func(b *Builder) { b.Exit("in") })
				note(b, Int(-1))
			})
			note(b, Int(1))
			b.Loop("l", func(b *Builder) {
				b.Set("i", Add(L("i"), Int(1)))
				b.If(Eq(L("i"), Int(3)), func(b *Builder) { b.Exit("out") })
			})
		})
		note(b, L("i"))
		b.Loop("outer", func(b *Builder) {
			b.Set("i", Add(L("i"), Int(1)))
			b.Loop("inner", func(b *Builder) {
				b.If(Lt(L("i"), Int(6)), func(b *Builder) { b.Continue("outer") })
				b.If(Eq(L("i"), Int(6)), func(b *Builder) { b.Break("outer") })
				b.Break("inner")
			})
			note(b, Int(-2))
		})
		note(b, L("i"))
		for k := int64(0); k < 2; k++ {
			b.Block("twin", func(b *Builder) {
				b.If(Eq(L("i"), Int(6)), func(b *Builder) { b.Exit("twin") })
				note(b, Int(-3))
			})
			note(b, Int(10+k))
		}
		b.Return(L("i"))
	})
	// Locals of nested blocks that an exit leaves before or after they are declared.
	m.AddFunc("scopes", nil, []Type{TInt}, nil, func(b *Builder) {
		b.Let("sum", Int(0))
		for k := int64(0); k < 2; k++ {
			b.Block("blk", func(b *Builder) {
				b.Let("a", Int(10+k))
				b.Set("sum", Add(L("sum"), L("a")))
				b.If(Eq(L("sum"), Int(10)), func(b *Builder) { b.Exit("blk") })
				b.Let("c", Int(100))
				b.Set("sum", Add(L("sum"), L("c")))
				b.Let("unread", Int(5))
				b.Let("written", Int(6))
				b.Set("written", Int(7))
			})
		}
		note(b, L("sum"))
		b.Return(L("sum"))
	})
	// Loop clauses (Post also after a Continue), an If with Init, an assignment of several places
	// from one call with a register and a discarded result, and a function with two results.
	m.AddFunc("pair", []Param{P("x", TInt)}, []Type{TInt, TInt}, nil, func(b *Builder) {
		b.Return(Add(L("x"), Int(1)), Bin("*", L("x"), Int(2)))
	})
	m.AddFunc("clauses", nil, []Type{TInt, TInt, TInt}, nil, func(b *Builder) {
		i, s := b.Var("i", TInt), b.Var("s", TInt)
		b.Var("q", TInt)
		b.For("l", Assign("i", Int(0)), Lt(i, Int(6)), Assign("i", Add(i, Int(1))), func(b *Builder) {
			b.If(Eq(Bin("%", i, Int(2)), Int(1)), func(b *Builder) { b.Continue("l") })
			b.Set("s", Add(s, i))
		})
		b.For("w", nil, Lt(s, Int(20)), nil, func(b *Builder) { b.Set("s", Add(s, Int(5))) })
		b.SetN([]Place{{Name: "pos", Reg: true}, {}}, C("pair", Int(4)))
		b.SetN([]Place{{}, {Name: "q"}}, C("pair", Int(4)))
		b.Append(&If{Init: Assign("s", Add(s, Int(100))), Cond: Lt(Int(0), s), Then: &Block{Stmts: []Stmt{
			&Do{Call: C("log.note", s)},
		}}, Else: &Block{Stmts: []Stmt{&If{Cond: Eq(s, Int(0)), Then: &Block{}}}}})
		b.Comment("a comment")
		b.Return(i, R("pos"), L("q"))
	})
	// && and || evaluate their right operand only when needed.
	m.AddFunc("shortcircuit", nil, []Type{TBool, TBool}, nil, func(b *Builder) {
		b.Let("a", And(C("log.flag", BoolC(false)), C("log.flag", BoolC(true))))
		b.Let("o", Bin("||", C("log.flag", BoolC(true)), C("log.flag", BoolC(false))))
		b.Return(L("a"), Not(Bin("&&", Not(L("o")), BoolC(true))))
	})
	// Recursion through functions.
	m.AddFunc("fib", []Param{P("n", TInt)}, []Type{TInt}, nil, func(b *Builder) {
		b.If(Lt(L("n"), Int(2)), func(b *Builder) { b.Return(L("n")) })
		b.Return(Add(C("fib", Bin("-", L("n"), Int(1))), C("fib", Bin("-", L("n"), Int(2)))))
	})
	m.AddFunc("calls", nil, []Type{TInt}, nil, func(b *Builder) { b.Return(C("fib", Int(15))) })
	// Handles: the null handle and boxes.
	m.AddFunc("handles", nil, []Type{TInt, TBool, TBool}, nil, func(b *Builder) {
		x := b.Var("x", box)
		b.Let("wasNull", Eq(x, Null(box)))
		b.Set("x", C("box.new", Int(42)))
		b.Let("y", C("box.new", Int(42)))
		b.Return(C("box.get", x), L("wasNull"), Bin("!=", x, L("y")))
	})
	// Specialization: one function printed for two sets of features.
	spec := m.AddFunc("special", nil, []Type{TInt}, []string{"extra"}, func(b *Builder) {
		b.Let("n", Int(1))
		b.If(F("extra"), func(b *Builder) {
			b.Set("n", Add(L("n"), Int(10)))
			note(b, L("n"))
		})
		b.Return(L("n"))
	})
	m.Funcs = m.Funcs[:len(m.Funcs)-1]
	for _, on := range [][]string{nil, {"extra"}} {
		s, err := pass.Specialize(m, spec, "special_"+strings.Join(append([]string{"x"}, on...), "_"), on)
		if err != nil {
			panic(err)
		}
		m.Funcs = append(m.Funcs, s)
	}
	if err := Verify(m); err != nil {
		panic(err)
	}
	return m
}

// Entries returns the functions of the module m without parameters, which a run calls in order.
func Entries(m *Module) []*Func {
	var out []*Func
	for _, f := range m.Funcs {
		if len(f.Params) == 0 {
			out = append(out, f)
		}
	}
	return out
}

// Want returns what a run prints: for each entry, the lines its operations print and then a line
// "<name> <result> ..." with its results.
func Want(m *Module) string {
	var b strings.Builder
	for _, f := range Entries(m) {
		mach := &machine{b: &b}
		res, err := Run(m, f, mach)
		if err != nil {
			panic(err)
		}
		b.WriteString(f.Name)
		for _, r := range res {
			fmt.Fprintf(&b, " %v", r)
		}
		b.WriteString("\n")
	}
	return b.String()
}

type box struct{ x int64 }

type machine struct{ b *strings.Builder }

func (m *machine) Call(o *Op, args []any) []any {
	switch o.Name {
	case "log.note":
		fmt.Fprintf(m.b, "note %v\n", args[0])
		return nil
	case "log.flag":
		fmt.Fprintf(m.b, "flag %v\n", args[0])
		return []any{args[0]}
	case "box.new":
		return []any{&box{args[0].(int64)}}
	case "box.get":
		return []any{args[0].(*box).x}
	}
	panic("printtest: no operation " + o.Name)
}
