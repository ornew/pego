//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
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
	prog, err := Compile(g, compile.Options{})
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
	t.Parallel()
	prog, choice := workGuardedChoiceProgram(t)
	c := newClosureWalker(&compile.Compiler{Prog: prog.Static()}, false)
	m := c.ExprForm(choice, compile.NewScope(), true)

	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		t.Run(unit.String()+"/budget", func(t *testing.T) {
			p := core.NewParser(&prog.sharedProgram, "z", unit, false)
			p.Control = core.NewWorkControl(nil, limits.Limits{MaxWork: workGuardedChoiceAlternatives})
			p.Silent = 1
			p.MaxDepth = workGuardedChoiceAlternatives + 1
			err := catchFatal(t, func() { _, _ = m(p) })
			var limit *limits.LimitError
			if !errors.As(err, &limit) || limit.Kind != limits.LimitWork || limit.Used != workGuardedChoiceAlternatives || limit.Requested != 1 {
				t.Fatalf("direct silent choice error=%#v, want rejected step after one charged peek and %d guard iterations", err, workGuardedChoiceAlternatives-1)
			}
		})

		t.Run(unit.String()+"/cancel", func(t *testing.T) {
			p := core.NewParser(&prog.sharedProgram, "z", unit, false)
			ctx := newPollingContext(1)
			p.Control = core.NewWorkControl(ctx, limits.Limits{})
			p.Silent = 1
			p.MaxDepth = workGuardedChoiceAlternatives + 1
			err := catchFatal(t, func() { _, _ = m(p) })
			if !errors.Is(err, context.Canceled) || p.Control.Used < core.WorkCheckInterval/2 || p.Control.Used >= core.WorkCheckInterval {
				t.Fatalf("direct silent choice cancellation err=%v steps=%d, want cancellation at a bounded step poll", err, p.Control.Used)
			}
		})
	}
}

func TestWorkGuardedChoiceExactSkipBudget(t *testing.T) {
	t.Parallel()
	prog, choice := workGuardedChoiceProgram(t)
	c := newClosureWalker(&compile.Compiler{Prog: prog.Static()}, false)
	m := c.ExprForm(choice, compile.NewScope(), false)
	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		for _, text := range []string{"z", ""} {
			for _, silent := range []bool{true, false} {
				t.Run(unit.String()+"/input="+strconv.Quote(text)+"/silent="+strconv.FormatBool(silent), func(t *testing.T) {
					p := core.NewParser(&prog.sharedProgram, text, unit, false)
					p.Control = core.NewWorkControl(nil, limits.Limits{MaxWork: workGuardedChoiceAlternatives + 1})
					p.MaxDepth = workGuardedChoiceAlternatives + 1
					if silent {
						p.Silent = 1
					} else {
						// An older failure position also suppresses expectations.
						p.Farthest = 1
					}
					if n, ok := m(p); n != nil || ok {
						t.Fatalf("rejected choice returned %v, %v", n, ok)
					}
					if p.Control.Used != workGuardedChoiceAlternatives+1 || len(p.Exp) != 0 || p.Pos != 0 {
						t.Fatalf("choice state work=%d expectations=%v pos=%d", p.Control.Used, p.Exp, p.Pos)
					}
					p.Control = core.NewWorkControl(nil, limits.Limits{MaxWork: workGuardedChoiceAlternatives})
					err := catchFatal(t, func() { _, _ = m(p) })
					var limit *limits.LimitError
					if !errors.As(err, &limit) || limit.Kind != limits.LimitWork || limit.Used != workGuardedChoiceAlternatives || limit.Requested != 1 || p.Control.Used != workGuardedChoiceAlternatives || len(p.Exp) != 0 {
						t.Fatalf("one-short choice error=%#v work=%d expectations=%v", err, p.Control.Used, p.Exp)
					}
				})
			}
		}
	}
}

func TestWorkGuardedChoicePassingMatcherCharge(t *testing.T) {
	t.Parallel()
	prog, _ := workGuardedChoiceProgram(t)
	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		for _, skip := range []bool{false, true} {
			t.Run(unit.String()+"/skip="+strconv.FormatBool(skip), func(t *testing.T) {
				alts := []grammar.Expr{&grammar.Literal{Value: "z"}}
				steps := uint64(3) // one peek, matcher dispatch, and literal probe, one unit each
				if skip {
					alts = append([]grammar.Expr{&grammar.Literal{Value: "x"}}, alts...)
					steps++
				}
				c := newClosureWalker(&compile.Compiler{Prog: prog.Static()}, false)
				m := c.ExprForm(&grammar.Choice{Alts: alts}, compile.NewScope(), false)
				p := core.NewParser(&prog.sharedProgram, "z", unit, false)
				p.Control = core.NewWorkControl(nil, limits.Limits{MaxWork: steps})
				p.Silent = 1
				if n, ok := m(p); n != nil || !ok || p.Pos != 1 || p.Control.Used != steps {
					t.Fatalf("matching choice node=%v ok=%v pos=%d work=%d", n, ok, p.Pos, p.Control.Used)
				}
			})
		}
	}
}

func TestWorkGuardedChoicePublicParityAndFatalRetry(t *testing.T) {
	t.Parallel()
	prog, _ := workGuardedChoiceProgram(t)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize), func(t *testing.T) {
					base := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
					for _, text := range []string{"z", "q", ""} {
						want, wantErr := prog.ParseWith("main", text, base)
						controlled := base
						controlled.Limits = limits.Limits{MaxWork: 1 << 20}
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

		var observed *core.Parser
		fallbackEntries, exits := 0, 0
		opts := ParseOptions{
			Backend:   backend,
			Unit:      unit,
			Recognize: recognize,
			Limits:    limits.Limits{MaxWork: 1 << 30},
			Trace: func(e TraceEvent) {
				if e.Kind == TraceEnter && e.Rule == "main" {
					observed = core.TraceParser(e)
					if cancel {
						core.TraceParser(e).Control.Ctx = newPollingContext(2)
					} else {
						core.TraceParser(e).Control.Limits.MaxWork = core.TraceParser(e).Control.Used + 32
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
		if n != nil || observed == nil || observed.Scratch != nil || observed.Control != nil {
			t.Fatalf("fatal parse node=%v err=%v parser=%v", n, gotErr, observed)
		}
		if cancel {
			if !errors.Is(gotErr, context.Canceled) {
				t.Fatalf("cancelled parse error=%v, want context cancellation", gotErr)
			}
		} else {
			var limit *limits.LimitError
			if !errors.As(gotErr, &limit) || limit.Kind != limits.LimitWork {
				t.Fatalf("budgeted parse error=%v, want work LimitError", gotErr)
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
