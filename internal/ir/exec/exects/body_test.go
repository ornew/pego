package exects

import (
	"strings"
	"testing"

	ir "github.com/ornew/pego/internal/ir/exec"
	"github.com/ornew/pego/internal/ir/exec/pass"
)

// bodyFunc returns a module with the operations of the body tests and the function f of the locals
// and body, verified.
func bodyFunc(t *testing.T, results []ir.Type, locals []ir.Param, body *ir.Block) (*ir.Module, *ir.Func) {
	t.Helper()
	m := ir.NewModule("test")
	m.AddComponent("log", "The calls made.")
	m.AddRegister("pos", ir.TInt, "The position.")
	m.AddHandle("node", "log", "A node.")
	m.AddOp(&ir.Op{Name: "note", Component: "log", Params: []ir.Param{ir.P("x", ir.TInt)}, Writes: []string{"log"}, Contract: "Records x."})
	m.AddOp(&ir.Op{Name: "flag", Component: "log", Results: []ir.Type{ir.TBool}, Writes: []string{"log"}, Contract: "Records a call and returns true."})
	f := &ir.Func{Name: "f", Results: results, Locals: locals, Body: body}
	m.Funcs = append(m.Funcs, f)
	if err := ir.Verify(m); err != nil {
		t.Fatal(err)
	}
	return m, f
}

func note(x ir.Expr) ir.Stmt { return &ir.Do{Call: ir.C("note", x)} }

func flag() ir.Expr { return ir.C("flag") }

// bodyTarget prints labels as L_ and their name, recording the order Body names them in.
func bodyTarget(named *[]string) *BodyTarget {
	return &BodyTarget{
		Reg: func(name string) string { return "p." + name },
		Label: func(name string) string {
			*named = append(*named, name)
			return "L_" + name
		},
		Op: func(c *ir.Call, _ bool) string { return c.Callee + "()" },
		Do: func(c *ir.Call, args func() []string, _ bool) string {
			return c.Callee + "(" + strings.Join(args(), ", ") + ");\n"
		},
		Pure: func(*ir.Call) bool { return false },
	}
}

// printBody prints the body of f with the stores of the int locals cand.
func printBody(m *ir.Module, f *ir.Func, cand []string, named *[]string) BodyResult {
	t := bodyTarget(named)
	st := pass.DeadStores(f, cand, func(x ir.Expr) bool { return Pure(x, t.Pure) })
	return Body(m, f, t, st)
}

func ints(names ...string) []ir.Param {
	var ps []ir.Param
	for _, n := range names {
		ps = append(ps, ir.P(n, ir.TInt))
	}
	return ps
}

// TestBodyDeadPoints checks that nothing prints after a jump up to the label it exits to, that the
// reads there do not keep an assignment, and that the body's own label is left to the caller.
func TestBodyDeadPoints(t *testing.T) {
	m, f := bodyFunc(t, nil, ints("a"), &ir.Block{Label: "fail", Stmts: []ir.Stmt{
		ir.Assign("a", ir.Int(1)),
		&ir.Block{Label: "in", Stmts: []ir.Stmt{
			&ir.Exit{Label: "in"},
			note(ir.L("a")),
		}},
		note(ir.Int(2)),
		&ir.Exit{Label: "fail"},
		note(ir.Int(3)),
	}})
	var named []string
	r := printBody(m, f, []string{"a"}, &named)
	want := "L_in: {\n  break L_in;\n}\nnote(2);\nbreak L_fail;\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
	if r.End || !r.Exited {
		t.Errorf("end %v, exited %v; want the end unreachable and the body exited", r.End, r.Exited)
	}
}

// TestBodyLoops checks the heads of loops and the reachability after them: after an unconditional
// loop that no jump leaves by its label, the point is unreachable.
func TestBodyLoops(t *testing.T) {
	m, f := bodyFunc(t, nil, ints("i"), &ir.Block{Label: "fail", Stmts: []ir.Stmt{
		&ir.Loop{Label: "c", Init: ir.Assign("i", ir.Int(0)), Cond: &ir.Binary{Op: "<", X: ir.L("i"), Y: ir.Int(3)},
			Post: ir.Assign("i", &ir.Binary{Op: "+", X: ir.L("i"), Y: ir.Int(1)}), Body: &ir.Block{Stmts: []ir.Stmt{note(ir.L("i"))}}},
		&ir.Loop{Label: "w", Cond: flag(), Body: &ir.Block{Stmts: []ir.Stmt{note(ir.Int(1))}}},
		&ir.Loop{Label: "u", Body: &ir.Block{Stmts: []ir.Stmt{&ir.Exit{Label: "fail"}}}},
		note(ir.Int(2)),
	}})
	var named []string
	r := printBody(m, f, []string{"i"}, &named)
	want := "for (i = 0; i < 3; i++) {\n  note(i);\n}\nwhile (flag()) {\n  note(1);\n}\nfor (;;) {\n  break L_fail;\n}\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
	if r.End {
		t.Errorf("the end is reachable after an unconditional loop that only exits the body")
	}
}

// TestBodyLabels checks that a label is named where its first jump prints, that a Continue names
// its label even at an unreachable point, and that a loop whose label a jump names is labeled.
func TestBodyLabels(t *testing.T) {
	m, f := bodyFunc(t, nil, nil, &ir.Block{Label: "fail", Stmts: []ir.Stmt{
		&ir.Loop{Label: "y", Body: &ir.Block{Stmts: []ir.Stmt{
			&ir.Loop{Label: "x", Body: &ir.Block{Stmts: []ir.Stmt{
				&ir.Break{Label: "x"},
				&ir.Continue{Label: "y"},
			}}},
		}}},
		note(ir.Int(1)),
	}})
	var named []string
	r := printBody(m, f, nil, &named)
	want := "L_y: for (;;) {\n  L_x: for (;;) {\n    break L_x;\n  }\n}\nnote(1);\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
	if got := strings.Join(named, " "); !strings.HasPrefix(got, "x y") {
		t.Errorf("labels named in the order %s; want x first, then y", got)
	}
	if !r.End || r.Exited {
		t.Errorf("end %v, exited %v; want the end reachable and the body not exited", r.End, r.Exited)
	}
}

// TestBodyIfs checks that an If with one statement and no else prints on one line, that a
// disjunction prints in parentheses and a negation of anything but a local wraps its operand.
func TestBodyIfs(t *testing.T) {
	m, f := bodyFunc(t, nil, ints("a"), &ir.Block{Label: "fail", Stmts: []ir.Stmt{
		ir.Assign("a", ir.Int(1)),
		&ir.If{Cond: &ir.Binary{Op: "||", X: &ir.Binary{Op: "==", X: ir.L("a"), Y: ir.Int(1)}, Y: &ir.Unary{Op: "!", X: flag()}},
			Then: &ir.Block{Stmts: []ir.Stmt{note(ir.Int(1))}}},
		&ir.If{Cond: flag(), Then: &ir.Block{Stmts: []ir.Stmt{note(ir.Int(2)), note(ir.Int(3))}},
			Else: &ir.Block{Stmts: []ir.Stmt{note(ir.Int(4))}}},
	}})
	var named []string
	r := printBody(m, f, []string{"a"}, &named)
	want := "a = 1;\nif ((a === 1 || !(flag()))) note(1);\nif (flag()) {\n  note(2);\n  note(3);\n} else {\n  note(4);\n}\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
}

// TestBodySets checks the assignments: an unread local's place is left out with its value printed
// alone unless pure, an increment prints as ++, and a register is assigned whatever reads it.
func TestBodySets(t *testing.T) {
	m, f := bodyFunc(t, nil, append(ints("a", "b", "c"), ir.P("k", ir.TBool)), &ir.Block{Label: "fail", Stmts: []ir.Stmt{
		&ir.Set{Dst: []ir.Place{{Name: "k"}}, Values: []ir.Expr{flag()}},
		ir.Assign("b", ir.Int(1)),
		ir.Assign("c", ir.Int(2)),
		ir.Assign("c", &ir.Binary{Op: "+", X: ir.L("c"), Y: ir.Int(1)}),
		ir.Assign("a", ir.Int(3)),
		ir.Assign("a", &ir.Binary{Op: "+", X: ir.L("a"), Y: ir.Int(1)}),
		&ir.Set{Dst: []ir.Place{{Name: "pos", Reg: true}}, Values: []ir.Expr{ir.L("c")}},
	}})
	var named []string
	r := printBody(m, f, []string{"a", "b", "c", "k"}, &named)
	want := "flag();\nc = 2;\nc++;\np.pos = c;\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
}

// TestBodyReturns checks that a return of a value and true returns the value, and one of false
// returns undefined.
func TestBodyReturns(t *testing.T) {
	node := ir.THandle("node")
	m, f := bodyFunc(t, []ir.Type{node, ir.TBool}, []ir.Param{ir.P("h", node)}, &ir.Block{Stmts: []ir.Stmt{
		&ir.If{Cond: flag(), Then: &ir.Block{Stmts: []ir.Stmt{&ir.Return{Values: []ir.Expr{ir.L("h"), ir.BoolC(true)}}}}},
		&ir.Return{Values: []ir.Expr{ir.Null(node), ir.BoolC(false)}},
		note(ir.Int(1)),
	}})
	var named []string
	r := printBody(m, f, []string{"h"}, &named)
	want := "if (flag()) return h;\nreturn undefined;\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
	if r.End {
		t.Errorf("the end is reachable after a return")
	}
}
