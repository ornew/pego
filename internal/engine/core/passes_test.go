package core

import (
	"fmt"
	"strings"
	"testing"
)

// passes runs the passes on m in turn, each followed by Analyze and Verify.
func passes(t *testing.T, m *Module, ps ...func(*Module)) {
	t.Helper()
	analyzed(t, m)
	for i, p := range ps {
		p(m)
		Analyze(m)
		if err := Verify(m); err != nil {
			t.Fatalf("after pass %d: %v", i, err)
		}
	}
}

func guardsPass(classes bool) func(*Module) { return func(m *Module) { Guards(m, classes) } }

func terminalsPass(m *Module) { Terminals(m, func(*Rule) bool { return true }) }

func valueFree(m *Module) *Module {
	for _, r := range m.Rules {
		r.Want = false
	}
	return m
}

func TestPassesGuardedClassRuns(t *testing.T) {
	// a = [a-z] [0-9] [x-y] "q" / "r" [a-z] [0-9]   (value-free)
	alt0 := seq(class('a', 'z'), class('0', '9'), class('x', 'y'), lit("q"))
	alt1 := seq(lit("r"), class('a', 'z'), class('0', '9'))
	m := valueFree(module(rule(0, "a", choice(alt0, alt1))))
	passes(t, m, guardsPass(true), Scans, ClassRuns)
	got := PrintNode(m, m.Rules[0].Body, false)
	want := `choice
  alt guard=[a-z] class
    seq
      class [a-z]
      classrun [0-9] [x-y]
      lit "q"
  alt guard='r'
    seq
      lit "r"
      classrun [a-z] [0-9]
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPassesTerminals(t *testing.T) {
	var alts []Node
	for _, w := range strings.Fields("ab cd ef gh ij kl mn op") {
		alts = append(alts, seq(lit(w[:1]), lit(w[1:])))
	}
	small := choice(lit("x"), lit("y"))
	body := seq(&Drop{Body: seq(lit("k"), choice(alts...))}, small, &Any{}) // any character is no terminal form
	body.Items[0].Attrs().Steps = 3
	m := valueFree(module(rule(0, "a", body)))
	passes(t, m, terminalsPass, guardsPass(false))
	term, ok := body.Items[0].(*Terminal)
	if !ok {
		t.Fatalf("17 leaves: %T, want a terminal", body.Items[0])
	}
	if term.Attrs().Steps != 3 {
		t.Errorf("terminal steps %d, want those of its body, 3", term.Attrs().Steps)
	}
	if body.Items[1] != small {
		t.Errorf("2 leaves: %T, want the choice", body.Items[1])
	}
	if small.Alts[0].Guard == nil {
		t.Errorf("no guard outside the terminal")
	}
	Walk(term.Body, func(n Node) {
		if c, ok := n.(*Choice); ok {
			for _, a := range c.Alts {
				if a.Guard != nil {
					t.Errorf("guard %s inside the terminal", firstString(m, a.Guard))
				}
			}
		}
	})
	if f := body.Attrs().First; f == nil || firstString(m, f) != "'k'" {
		t.Errorf("first through the terminal: %+v", f)
	}
}

// TestPassesTerminalWant checks that Terminals leaves expressions whose value is wanted.
func TestPassesTerminalWant(t *testing.T) {
	var alts []Node
	for _, w := range strings.Fields("ab cd ef gh ij kl mn op") {
		alts = append(alts, seq(lit(w[:1]), lit(w[1:])))
	}
	body := choice(alts...)
	body.Attrs().Want = true
	m := module(rule(0, "a", body))
	passes(t, m, terminalsPass)
	if m.Rules[0].Body != body {
		t.Errorf("wanted choice: %T, want the choice", m.Rules[0].Body)
	}
}

// TestPassesTerminalContext checks that a lookahead or a discarded expression in a sequence that
// builds a value is no Terminal, though its value-free body is one.
func TestPassesTerminalContext(t *testing.T) {
	keywords := func() Node {
		var alts []Node
		for _, w := range strings.Fields("ab cd ef gh ij kl mn op") {
			alts = append(alts, seq(lit(w[:1]), lit(w[1:])))
		}
		return choice(alts...)
	}
	not := &Look{Body: keywords(), Neg: true}
	drop := &Drop{Body: keywords()}
	x := lit("x")
	x.Attrs().Want = true
	body := seq(not, drop, x)
	body.Attrs().Want = true
	m := module(rule(0, "a", body))
	m.Rules[0].Want = true
	passes(t, m, terminalsPass)
	if body.Items[0] != not || body.Items[1] != drop {
		t.Fatalf("items %T %T, want the lookahead and the discarded expression", body.Items[0], body.Items[1])
	}
	if _, ok := not.Body.(*Terminal); !ok {
		t.Errorf("lookahead body %T, want a terminal", not.Body)
	}
	if _, ok := drop.Body.(*Terminal); !ok {
		t.Errorf("discarded body %T, want a terminal", drop.Body)
	}
	// In a value-free sequence, the lookahead itself is one.
	not = &Look{Body: keywords(), Neg: true}
	body = seq(not, &Any{}) // any character is no terminal form
	m = valueFree(module(rule(0, "a", body)))
	passes(t, m, terminalsPass)
	if _, ok := body.Items[0].(*Terminal); !ok {
		t.Errorf("value-free lookahead %T, want a terminal", body.Items[0])
	}
}

// TestPassesTerminalInStream checks that the passes reach the element of a #stream repetition, so
// that a discarded expression of the element is a Terminal as it is in any repetition.
func TestPassesTerminalInStream(t *testing.T) {
	var alts []Node
	for _, w := range strings.Fields("ab cd ef gh ij kl mn op") {
		alts = append(alts, seq(lit(w[:1]), lit(w[1:])))
	}
	drop := &Drop{Body: choice(alts...)}
	x := lit("x")
	x.Attrs().Want = true
	body := seq(drop, x)
	body.Attrs().Want = true
	rep := &Repeat{Max: -1, Body: body}
	rep.Attrs().Want = true
	st := &Stream{Repeat: rep}
	st.Attrs().Want = true
	m := module(rule(0, "a", st))
	m.Rules[0].Want = true
	m.Starts = []int{0}
	passes(t, m, terminalsPass)
	if _, ok := drop.Body.(*Terminal); !ok {
		t.Errorf("discarded body in a stream element %T, want a terminal", drop.Body)
	}
}

// TestPassesOperatorLines checks that the passes reach the lines of an Operators Rule.
func TestPassesOperatorLines(t *testing.T) {
	operand := choice(class('0', '9'), lit("("))
	op := &Operator{ID: 0, Kind: "infix", Level: 1, Line: &OpLine{Body: &Repeat{Max: -1, Body: class('a', 'z')}}}
	m := valueFree(module(rule(0, "e", &OperatorsRule{Operands: []*OpLine{{Body: operand}}, Ops: []*Operator{op}})))
	passes(t, m, guardsPass(true), Scans)
	if operand.Alts[0].Guard == nil || !operand.Alts[0].GuardedClass || operand.Alts[1].Guard == nil {
		t.Errorf("operand guards: %+v %+v", operand.Alts[0], operand.Alts[1])
	}
	if _, ok := op.Line.Body.(*Scan); !ok {
		t.Errorf("operator line: %T, want a scan", op.Line.Body)
	}
}

// TestPassesScanPreconditions checks that Scans leaves repetitions whose elements have values,
// scopes or projections.
func TestPassesScanPreconditions(t *testing.T) {
	wanted := &Repeat{Max: -1, Body: class('a', 'z')}
	wanted.Attrs().Want = true
	scoped := &Repeat{Max: -1, Scope: []Slot{}, Body: class('a', 'z')}
	projected := &Repeat{Max: -1, Project: "x", Body: class('a', 'z')}
	plain := &Repeat{Min: 1, Max: 3, Body: &Any{}}
	m := module(rule(0, "a", seq(wanted, scoped, projected, plain)))
	m.Rules[0].Want = false
	passes(t, m, Scans)
	items := m.Rules[0].Body.(*Seq).Items
	for i, want := range []string{"*core.Repeat", "*core.Repeat", "*core.Repeat", "*core.Scan"} {
		if got := fmt.Sprintf("%T", items[i]); got != want {
			t.Errorf("item %d: %s, want %s", i, got, want)
		}
	}
	if s := items[3].(*Scan); s.Class != nil || s.Min != 1 || s.Max != 3 {
		t.Errorf("scan of any character: %+v", s)
	}
}
