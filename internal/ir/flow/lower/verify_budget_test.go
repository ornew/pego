package lower

import (
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/ir/core"
	"github.com/ornew/zeroparse/internal/ir/flow"
)

// stripSteps returns ss without the Step statements, at any depth.
func stripSteps(ss []flow.Stmt) []flow.Stmt {
	var out []flow.Stmt
	for _, s := range ss {
		switch s := s.(type) {
		case *flow.Step:
			continue
		case *flow.Block:
			s.Body = stripSteps(s.Body)
		case *flow.Loop:
			s.Body = stripSteps(s.Body)
		case *flow.If:
			s.Then, s.Else = stripSteps(s.Then), stripSteps(s.Else)
		}
		out = append(out, s)
	}
	return out
}

// TestVerifyBudgetCharges checks that a function lowered with budget controls charges before it consumes
// input and in every iteration of a repetition, and that Verify rejects the same function without
// its Step statements, and a hand-built loop whose iteration charges on one path only.
func TestVerifyBudgetCharges(t *testing.T) {
	for _, c := range []struct {
		name string
		body core.Node
	}{
		{"literal", &core.Lit{Text: "a"}},
		{"star of literals", &core.Repeat{Min: 0, Max: -1, Body: &core.Lit{Text: "ab"}}},
		{"plus of a sequence", &core.Repeat{Min: 1, Max: -1, Body: &core.Seq{Items: []core.Node{&core.Lit{Text: "a"}, &core.Lit{Text: "b"}}}}},
		{"choice", alts(&core.Lit{Text: "a"}, &core.Lit{Text: "b"})},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, r := valueFree(c.body)
			f, err := Lower(m, r, "r", Options{Budget: true})
			if err != nil {
				t.Fatal(err)
			}
			if !f.Budget {
				t.Fatal("the function does not record that it is lowered with budget controls")
			}
			if !f.Entered {
				t.Fatal("the function of a flat rule does not take its entry as charged by its caller")
			}
			f.Body = stripSteps(f.Body)
			f.Entered = false // without the step the caller charges, nothing charges the entry
			err = flow.Verify(f)
			if err == nil || !strings.Contains(err.Error(), "spend:") {
				t.Errorf("Verify of the function without its steps: got %v, want a spend error", err)
			}
		})
	}
	// The caller charges the entry of a framed function, and a repetition charges once per iteration: without
	// the iteration step, the entry charge does not cover the loop.
	for _, body := range []core.Node{
		&core.Repeat{Min: 0, Max: -1, Body: &core.Lit{Text: "ab"}},
		&core.Repeat{Min: 1, Max: 3, Body: &core.Lit{Text: "ab"}},
	} {
		m, r := valueFree(body)
		f, err := LowerFrame(m, r, body, nil, false, "r", Options{Budget: true})
		if err != nil {
			t.Fatal(err)
		}
		if !f.Entered {
			t.Fatal("the framed function does not take its entry as charged by its caller")
		}
		for _, s := range f.Body {
			if lp, ok := s.(*flow.Loop); ok {
				if _, ok := lp.Body[0].(*flow.Step); !ok {
					t.Errorf("a repetition does not charge first in its iteration:\n%s", flow.String(f))
				}
				lp.Body = stripSteps(lp.Body)
			}
		}
		if err := flow.Verify(f); err == nil || !strings.Contains(err.Error(), "spend:") {
			t.Errorf("Verify of a repetition without its iteration step: got %v, want a spend error", err)
		}
	}
	loop := &flow.Label{Name: "loop"}
	newFunc := func(body ...flow.Stmt) *flow.Func {
		return &flow.Func{Name: "f", Budget: true, Body: body, Fail: &flow.Label{Name: "fail"},
			Start: &flow.Var{Name: "start", Type: flow.TypeInt, Entry: true}, Result: &flow.Var{Name: "v", Type: flow.TypeValue, Entry: true},
			Ctx: &flow.Var{Name: "c", Type: flow.TypeCtx, Entry: true}}
	}
	ok := &flow.Var{Name: "ok", Type: flow.TypeBool}
	oneSided := newFunc(&flow.Loop{Label: loop, Body: []flow.Stmt{
		&flow.If{Cond: &flow.Use{V: ok}, Then: []flow.Stmt{&flow.Exit{To: loop}}},
		&flow.If{Cond: &flow.Use{V: ok}, Then: []flow.Stmt{&flow.Step{}, &flow.Continue{To: loop}}, Else: []flow.Stmt{&flow.Continue{To: loop}}},
	}})
	oneSided.Vars = []*flow.Var{ok}
	if err := flow.Verify(oneSided); err == nil || !strings.Contains(err.Error(), "continues without charging") {
		t.Errorf("a loop that charges on one path: got %v", err)
	}
	always := newFunc(&flow.Loop{Label: loop, Body: []flow.Stmt{
		&flow.If{Cond: &flow.Use{V: ok}, Then: []flow.Stmt{&flow.Exit{To: loop}}},
		&flow.Step{}, &flow.Continue{To: loop}}})
	always.Vars = []*flow.Var{ok}
	if err := flow.Verify(always); err != nil {
		t.Errorf("a loop that charges before it continues: %v", err)
	}
	early := newFunc(&flow.Advance{N: &flow.Int{N: 1}})
	if err := flow.Verify(early); err == nil || !strings.Contains(err.Error(), "Advance before a charge") {
		t.Errorf("an Advance before a charge: got %v", err)
	}
	entered := newFunc(&flow.Advance{N: &flow.Int{N: 1}})
	entered.Entered = true
	if err := flow.Verify(entered); err != nil {
		t.Errorf("a function whose caller charged its entry: %v", err)
	}
	// A loop that ends by its bound, perhaps before any iteration, leaves the state uncharged even when
	// its only exit has charged.
	bounded := newFunc(&flow.Loop{Label: loop, Counter: ok, Max: 3, Body: []flow.Stmt{
		&flow.If{Cond: &flow.Use{V: ok}, Then: []flow.Stmt{&flow.Step{}, &flow.Exit{To: loop}}},
		&flow.Step{}}}, &flow.Advance{N: &flow.Int{N: 1}})
	bounded.Vars = []*flow.Var{ok}
	if err := flow.Verify(bounded); err == nil || !strings.Contains(err.Error(), "Advance before a charge") {
		t.Errorf("an Advance after a bounded loop that may run no iteration: got %v", err)
	}
}

// TestLowerRejectsBudgetWithLeanClasses checks that the class options that charge no spend for the input
// cannot be combined with Options.Budget.
func TestLowerRejectsBudgetWithLeanClasses(t *testing.T) {
	for _, opts := range []Options{{Budget: true, ClassTables: true}, {Budget: true, GuardedClasses: true}} {
		m, r := valueFree(&core.Lit{Text: "a"})
		if _, err := Lower(m, r, "r", opts); err == nil {
			t.Errorf("Lower accepted %+v", opts)
		}
	}
	m, r := valueFree(&core.Lit{Text: "a"})
	if _, err := Lower(m, r, "r", Options{ClassTables: true, GuardedClasses: true}); err != nil {
		t.Error(err)
	}
}

// TestLowerFrameRejectsBudgetWithLeanClasses is the same check for the framed lowering.
func TestLowerFrameRejectsBudgetWithLeanClasses(t *testing.T) {
	for _, opts := range []Options{{Budget: true, ClassTables: true}, {Budget: true, GuardedClasses: true}} {
		m, r := valueFree(&core.Lit{Text: "a"})
		for name, lower := range map[string]func() (*flow.Func, error){
			"LowerFrame": func() (*flow.Func, error) { return LowerFrame(m, r, r.Body, nil, false, "r", opts) },
		} {
			if _, err := lower(); err == nil || !strings.Contains(err.Error(), "Options.Budget") {
				t.Errorf("%s accepted %+v: %v", name, opts, err)
			}
		}
	}
}
