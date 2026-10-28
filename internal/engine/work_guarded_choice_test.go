//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/value"
)

const workGuardedChoiceAlternatives = 512

func workGuardedChoiceExpr() *grammar.Choice {
	alts := make([]grammar.Expr, workGuardedChoiceAlternatives)
	for i := range alts {
		alts[i] = &grammar.Literal{Value: string(rune(0x1000 + i))}
	}
	return &grammar.Choice{Alts: alts}
}

func workGuardedChoiceProgram(t *testing.T) (*Program, *grammar.Choice) {
	t.Helper()
	choice := workGuardedChoiceExpr()
	main := &grammar.Choice{Alts: []grammar.Expr{
		&grammar.Seq{Items: []grammar.Expr{
			&grammar.Not{Expr: choice},
			&grammar.Literal{Value: "z"},
		}},
		&grammar.Ref{Name: "fallback"},
	}}
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{
		&grammar.RuleDef{Name: "main", Expr: main},
		&grammar.RuleDef{Name: "fallback", Expr: &grammar.Literal{Value: "z"}},
	}}}
	starttest.MarkAll(g)
	prog, err := Compile(g, Options{})
	if err != nil {
		t.Fatalf("compile guarded-choice grammar: %v", err)
	}
	return prog, choice
}

func workGuardedChoiceSnapshot(n *value.Node) *value.Node {
	if n == nil {
		return nil
	}
	copy := *n
	if value.D(n).Children != nil {
		value.D(&copy).Children = make([]*value.Node, len(value.D(n).Children))
		for i, child := range value.D(n).Children {
			value.D(&copy).Children[i] = workGuardedChoiceSnapshot(child)
		}
	}
	value.D(&copy).Fields = append(value.Fields(nil), value.D(n).Fields...)
	for i := range value.D(&copy).Fields {
		if child, ok := value.D(&copy).Fields[i].Value.(*value.Node); ok {
			value.D(&copy).Fields[i].Value = workGuardedChoiceSnapshot(child)
		}
	}
	return &copy
}

func TestWorkGuardedChoiceSilentSkipLoopBudgetAndCancellation(t *testing.T) {
	prog, choice := workGuardedChoiceProgram(t)
	c := newClosureWalker(&compiler{prog: prog}, false)
	m := c.exprForm(choice, newScope(), true)

	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		t.Run(unit.String()+"/budget", func(t *testing.T) {
			p := newParser(prog, "z", unit, false)
			p.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: workGuardedChoiceAlternatives})
			p.silent = 1
			p.maxDepth = workGuardedChoiceAlternatives + 1
			err := catchFatal(t, func() { _, _ = m(p) })
			var limit *limits.WorkLimitError
			if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != workGuardedChoiceAlternatives || limit.Requested != 1 {
				t.Fatalf("direct silent choice error=%#v, want rejected step after one charged peek and %d guard iterations", err, workGuardedChoiceAlternatives-1)
			}
		})

		t.Run(unit.String()+"/cancel", func(t *testing.T) {
			p := newParser(prog, "z", unit, false)
			ctx := newPollingContext(1)
			p.control = newWorkControl(ctx, limits.WorkLimits{})
			p.silent = 1
			p.maxDepth = workGuardedChoiceAlternatives + 1
			err := catchFatal(t, func() { _, _ = m(p) })
			if !errors.Is(err, context.Canceled) || p.control.stepsUsed < workCheckStepInterval/2 || p.control.stepsUsed >= workCheckStepInterval {
				t.Fatalf("direct silent choice cancellation err=%v steps=%d, want cancellation at a bounded step poll", err, p.control.stepsUsed)
			}
		})
	}
}

func TestWorkGuardedChoiceExactSkipBudget(t *testing.T) {
	prog, choice := workGuardedChoiceProgram(t)
	c := newClosureWalker(&compiler{prog: prog}, false)
	m := c.exprForm(choice, newScope(), false)
	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		for _, text := range []string{"z", ""} {
			for _, silent := range []bool{true, false} {
				t.Run(unit.String()+"/input="+strconv.Quote(text)+"/silent="+strconv.FormatBool(silent), func(t *testing.T) {
					p := newParser(prog, text, unit, false)
					p.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1, MaxSteps: workGuardedChoiceAlternatives + 1})
					p.maxDepth = workGuardedChoiceAlternatives + 1
					if silent {
						p.silent = 1
					} else {
						// An older failure position also suppresses expectations.
						p.farthest = 1
					}
					if n, ok := m(p); n != nil || ok {
						t.Fatalf("rejected choice returned %v, %v", n, ok)
					}
					if p.control.stepsUsed != workGuardedChoiceAlternatives+1 || p.control.inputUsed != 1 || len(p.exp) != 0 || p.pos != 0 {
						t.Fatalf("choice state steps=%d input=%d expectations=%v pos=%d", p.control.stepsUsed, p.control.inputUsed, p.exp, p.pos)
					}
					p.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1, MaxSteps: workGuardedChoiceAlternatives})
					err := catchFatal(t, func() { _, _ = m(p) })
					var limit *limits.WorkLimitError
					if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit || limit.Used != workGuardedChoiceAlternatives || limit.Requested != 1 || p.control.inputUsed != 1 || len(p.exp) != 0 {
						t.Fatalf("one-short choice error=%#v input=%d expectations=%v", err, p.control.inputUsed, p.exp)
					}
				})
			}
		}
	}
}

func TestWorkGuardedChoicePassingMatcherCharge(t *testing.T) {
	prog, _ := workGuardedChoiceProgram(t)
	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		for _, skip := range []bool{false, true} {
			t.Run(unit.String()+"/skip="+strconv.FormatBool(skip), func(t *testing.T) {
				alts := []grammar.Expr{&grammar.Literal{Value: "z"}}
				steps := uint64(3) // one peek, matcher dispatch, and literal probe
				if skip {
					alts = append([]grammar.Expr{&grammar.Literal{Value: "x"}}, alts...)
					steps++
				}
				c := newClosureWalker(&compiler{prog: prog}, false)
				m := c.exprForm(&grammar.Choice{Alts: alts}, newScope(), false)
				p := newParser(prog, "z", unit, false)
				p.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 2, MaxSteps: steps})
				p.silent = 1
				if n, ok := m(p); n != nil || !ok || p.pos != 1 || p.control.stepsUsed != steps || p.control.inputUsed != 2 {
					t.Fatalf("matching choice node=%v ok=%v pos=%d steps=%d input=%d", n, ok, p.pos, p.control.stepsUsed, p.control.inputUsed)
				}
			})
		}
	}
}

func TestWorkGuardedChoicePublicParityAndFatalRetry(t *testing.T) {
	prog, _ := workGuardedChoiceProgram(t)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize), func(t *testing.T) {
					base := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
					for _, text := range []string{"z", "q", ""} {
						want, wantErr := prog.ParseWith("main", text, base)
						controlled := base
						controlled.WorkLimits = limits.WorkLimits{MaxInputWork: 1 << 20, MaxSteps: 1 << 20}
						got, gotErr := prog.ParseWith("main", text, controlled)
						if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
							t.Fatalf("generous controls changed result: got=%#v/%#v want=%#v/%#v", got, gotErr, want, wantErr)
						}
						if text == "z" && (wantErr != nil || !recognize && want == nil || recognize && want != nil) {
							t.Fatalf("fixture result node=%v err=%v recognize=%v", want, wantErr, recognize)
						}
						if text != "z" && (wantErr == nil || want != nil) {
							t.Fatalf("fixture accepted %q: node=%v err=%v", text, want, wantErr)
						}
					}
				})
			}
			for _, recognize := range []bool{false, true} {
				workGuardedChoiceFatalAndRetry(t, prog, backend, unit, recognize, false)
				workGuardedChoiceFatalAndRetry(t, prog, backend, unit, recognize, true)
			}
		}
	}
}

func workGuardedChoiceFatalAndRetry(t *testing.T, prog *Program, backend Backend, unit PositionUnit, recognize, cancel bool) {
	t.Helper()
	name := "budget"
	if cancel {
		name = "cancel"
	}
	t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize)+"/"+name, func(t *testing.T) {
		retained, err := prog.ParseWith("main", "z", ParseOptions{Backend: backend, Unit: unit})
		if err != nil || retained == nil {
			t.Fatalf("baseline parse: node=%v err=%v", retained, err)
		}
		snapshot := workGuardedChoiceSnapshot(retained)

		var observed *parser
		fallbackEntries, exits := 0, 0
		opts := ParseOptions{
			Backend:    backend,
			Unit:       unit,
			Recognize:  recognize,
			WorkLimits: limits.WorkLimits{MaxSteps: 1 << 30},
			Trace: func(e TraceEvent) {
				if e.Kind == TraceEnter && e.Rule == "main" {
					observed = e.p
					if cancel {
						e.p.control.ctx = newPollingContext(2)
					} else {
						e.p.control.limits.MaxSteps = e.p.control.stepsUsed + 32
					}
				}
				if e.Kind == TraceEnter && e.Rule == "fallback" {
					fallbackEntries++
				}
				if e.Kind == TraceExit {
					exits++
				}
			},
		}
		n, gotErr := prog.ParseWith("main", "z", opts)
		if n != nil || observed == nil || observed.scratch != nil || observed.control != nil {
			t.Fatalf("fatal parse node=%v err=%v parser=%v", n, gotErr, observed)
		}
		if cancel {
			if !errors.Is(gotErr, context.Canceled) {
				t.Fatalf("cancelled parse error=%v, want context cancellation", gotErr)
			}
		} else {
			var limit *limits.WorkLimitError
			if !errors.As(gotErr, &limit) || limit.Kind != limits.StepWorkLimit {
				t.Fatalf("budgeted parse error=%v, want step WorkLimitError", gotErr)
			}
		}
		if fallbackEntries != 0 || exits != 0 {
			t.Fatalf("fatal work entered fallback %d times or emitted %d exits", fallbackEntries, exits)
		}
		if !reflect.DeepEqual(retained, snapshot) {
			t.Fatal("fatal parse mutated a node retained from an earlier successful parse")
		}

		retry, retryErr := prog.ParseWith("main", "z", ParseOptions{Backend: backend, Unit: unit})
		if retryErr != nil || !reflect.DeepEqual(retry, snapshot) || !reflect.DeepEqual(retained, snapshot) {
			t.Fatalf("pool retry node=%v err=%v; retained=%v", retry, retryErr, retained)
		}
	})
}
