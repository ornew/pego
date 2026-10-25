package flow

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
)

// stripSteps returns ss without the Step statements, at any depth.
func stripSteps(ss []Stmt) []Stmt {
	var out []Stmt
	for _, s := range ss {
		switch s := s.(type) {
		case *Step:
			continue
		case *Block:
			s.Body = stripSteps(s.Body)
		case *Loop:
			s.Body = stripSteps(s.Body)
		case *If:
			s.Then, s.Else = stripSteps(s.Then), stripSteps(s.Else)
		}
		out = append(out, s)
	}
	return out
}

// TestVerifyWorkCharges checks that a function lowered with work controls charges before it consumes
// input and in every iteration of a repetition, and that Verify rejects the same function without
// its Step statements, and a hand-built loop whose iteration charges on one path only.
func TestVerifyWorkCharges(t *testing.T) {
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
			f, err := Lower(m, r, "r", Options{Work: true})
			if err != nil {
				t.Fatal(err)
			}
			if !f.Work {
				t.Fatal("the function does not record that it is lowered with work controls")
			}
			f.Body = stripSteps(f.Body)
			err = Verify(f)
			if err == nil || !strings.Contains(err.Error(), "work:") {
				t.Errorf("Verify of the function without its steps: got %v, want a work error", err)
			}
		})
	}
	loop := &Label{Name: "loop"}
	newFunc := func(body ...Stmt) *Func {
		return &Func{Name: "f", Work: true, Body: body, Fail: &Label{Name: "fail"},
			Start: &Var{Name: "start", Type: TypeInt, Entry: true}, Result: &Var{Name: "v", Type: TypeValue, Entry: true},
			Ctx: &Var{Name: "c", Type: TypeCtx, Entry: true}}
	}
	ok := &Var{Name: "ok", Type: TypeBool}
	oneSided := newFunc(&Loop{Label: loop, Body: []Stmt{
		&If{Cond: &Use{V: ok}, Then: []Stmt{&Exit{To: loop}}},
		&If{Cond: &Use{V: ok}, Then: []Stmt{&Step{}, &Continue{To: loop}}, Else: []Stmt{&Continue{To: loop}}},
	}})
	oneSided.Vars = []*Var{ok}
	if err := Verify(oneSided); err == nil || !strings.Contains(err.Error(), "continues without charging") {
		t.Errorf("a loop that charges on one path: got %v", err)
	}
	always := newFunc(&Loop{Label: loop, Body: []Stmt{
		&If{Cond: &Use{V: ok}, Then: []Stmt{&Exit{To: loop}}},
		&Step{}, &Continue{To: loop}}})
	always.Vars = []*Var{ok}
	if err := Verify(always); err != nil {
		t.Errorf("a loop that charges before it continues: %v", err)
	}
	early := newFunc(&Advance{N: &Int{N: 1}})
	if err := Verify(early); err == nil || !strings.Contains(err.Error(), "Advance before a charge") {
		t.Errorf("an Advance before a charge: got %v", err)
	}
	entered := newFunc(&Advance{N: &Int{N: 1}})
	entered.Entered = true
	if err := Verify(entered); err != nil {
		t.Errorf("a function whose caller charged its entry: %v", err)
	}
	// A loop that ends by its bound, perhaps before any iteration, leaves the state uncharged even when
	// its only exit has charged.
	bounded := newFunc(&Loop{Label: loop, Counter: ok, Max: 3, Body: []Stmt{
		&If{Cond: &Use{V: ok}, Then: []Stmt{&Step{}, &Exit{To: loop}}},
		&Step{}}}, &Advance{N: &Int{N: 1}})
	bounded.Vars = []*Var{ok}
	if err := Verify(bounded); err == nil || !strings.Contains(err.Error(), "Advance before a charge") {
		t.Errorf("an Advance after a bounded loop that may run no iteration: got %v", err)
	}
}
