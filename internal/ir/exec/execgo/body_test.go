package execgo

import (
	"strings"
	"testing"

	ir "github.com/ornew/zeroparse/internal/ir/exec"
)

// bodyModule returns a module with the operations of the body tests and the function f of int
// locals names and body.
func bodyModule(t *testing.T, names []string, body ...ir.Stmt) (*ir.Module, *ir.Func) {
	t.Helper()
	m := ir.NewModule("test", "typed")
	m.AddComponent("log", "The calls made.")
	m.AddOp(&ir.Op{Name: "log.note", Component: "log", Params: []ir.Param{ir.P("x", ir.TInt)}, Writes: []string{"log"}, Contract: "Records x."})
	m.AddOp(&ir.Op{Name: "log.flag", Component: "log", Results: []ir.Type{ir.TBool}, Writes: []string{"log"}, Contract: "Records a call and returns true."})
	f := &ir.Func{Name: "f", Body: &ir.Block{Label: "fail", Stmts: body}}
	for _, n := range names {
		f.Locals = append(f.Locals, ir.P(n, ir.TInt))
	}
	m.Funcs = append(m.Funcs, f)
	if err := ir.Verify(m); err != nil {
		t.Fatal(err)
	}
	return m, f
}

func note(x ir.Expr) ir.Stmt { return &ir.Do{Call: ir.C("log.note", x)} }

// TestBodyDeadPoints checks that nothing prints after a jump up to the label it exits to, and that
// the reads there do not keep an assignment.
func TestBodyDeadPoints(t *testing.T) {
	m, f := bodyModule(t, []string{"a"},
		ir.Assign("a", ir.Int(1)),
		&ir.Block{Label: "in", Stmts: []ir.Stmt{
			&ir.Exit{Label: "in"},
			note(ir.L("a")),
		}},
		note(ir.Int(2)),
		&ir.Exit{Label: "fail"},
		note(ir.Int(3)),
	)
	r := Body(m, f, testTarget, []string{"a"})
	want := "goto in\nin:\np.note(2)\ngoto fail\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
	if !r.Stores.Dead["a"] || r.End || !r.Exited {
		t.Errorf("dead %v, end %v, exited %v; want a dead, the end unreachable and the body exited", r.Stores.Dead, r.End, r.Exited)
	}
}

// TestBodyLoopBraces checks that a loop at an unreachable point keeps its braces and makes the point
// after it reachable.
func TestBodyLoopBraces(t *testing.T) {
	m, f := bodyModule(t, nil,
		&ir.Exit{Label: "fail"},
		&ir.Loop{Label: "l", Body: &ir.Block{Stmts: []ir.Stmt{note(ir.Int(1)), &ir.Break{Label: "l"}}}},
		note(ir.Int(2)),
	)
	r := Body(m, f, testTarget, nil)
	want := "goto fail\nfor {\n}\np.note(2)\n"
	if r.Text != want {
		t.Errorf("body\n%s\nwant\n%s", r.Text, want)
	}
	if !r.End {
		t.Error("the point after the loop is unreachable")
	}
}

// TestNegateAndPrec checks the hooks that negate an operation and give the precedence of its text.
func TestNegateAndPrec(t *testing.T) {
	m, f := bodyModule(t, []string{"n"},
		&ir.If{Cond: &ir.Unary{Op: "!", X: ir.C("log.flag")}, Then: &ir.Block{Stmts: []ir.Stmt{note(ir.Int(1))}}},
		&ir.If{Cond: &ir.Binary{Op: "&&", X: ir.C("log.flag"), Y: ir.C("log.flag")}, Then: &ir.Block{Stmts: []ir.Stmt{note(ir.Int(2))}}},
	)
	tg := *testTarget
	tg.Op = func(c *ir.Call, args []string) string {
		if c.Callee == "log.flag" {
			return "p.Pos == 0"
		}
		return testTarget.Op(c, args)
	}
	tg.Negate = func(c *ir.Call, args []string) (string, bool) {
		if c.Callee == "log.flag" {
			return "p.Pos != 0", true
		}
		return "", false
	}
	tg.Prec = func(text string) int {
		if strings.Contains(text, "==") {
			return 3
		}
		return 5
	}
	r := Body(m, f, &tg, nil)
	for _, want := range []string{"if p.Pos != 0 {\n", "if p.Pos == 0 && p.Pos == 0 {\n"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("body lacks %q:\n%s", want, r.Text)
		}
	}
}

func TestVarLines(t *testing.T) {
	vs := []Var{{"a", "int"}, {"ok", "bool"}, {"b", "int"}, {"c", "int"}}
	got := VarLines(vs, map[string]bool{"b": true}, map[string]bool{"c": true, "ok": true})
	want := "var a, c int\n_ = c\nvar ok bool\n_ = ok\n"
	if got != want {
		t.Errorf("VarLines\n%s\nwant\n%s", got, want)
	}
}
