//go:build !pego_budget_controls_reference

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

const budgetGuardedChoiceAlternatives = 512

func budgetGuardedChoiceExpr() *grammar.Choice {
	alts := make([]grammar.Expr, budgetGuardedChoiceAlternatives)
	for i := range alts {
		alts[i] = &grammar.Literal{Value: string(rune(0x1000 + i))}
	}
	return &grammar.Choice{Alts: alts}
}

func budgetGuardedChoiceProgram(t *testing.T) (*Program, *grammar.Choice) {
	t.Helper()
	choice := budgetGuardedChoiceExpr()
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

func budgetGuardedChoiceSnapshot(n *value.Node) *value.Node {
	if n == nil {
		return nil
	}
	copy := *n
	if value.D(n).Children != nil {
		value.D(&copy).Children = make([]*value.Node, len(value.D(n).Children))
		for i, child := range value.D(n).Children {
			value.D(&copy).Children[i] = budgetGuardedChoiceSnapshot(child)
		}
	}
	value.D(&copy).Fields = append(value.Fields(nil), value.D(n).Fields...)
	for i := range value.D(&copy).Fields {
		if child, ok := value.D(&copy).Fields[i].Value.(*value.Node); ok {
			value.D(&copy).Fields[i].Value = budgetGuardedChoiceSnapshot(child)
		}
	}
	return &copy
}

// A guarded choice is straight-line code: skipping its alternatives charges nothing, and the one
// peek that reads the input is its only charge. The size of the grammar bounds the skips between two
// charges, as it bounds the straight-line code of a generated rule.
func TestBudgetGuardedChoiceSilentSkipLoopBudgetAndCancellation(t *testing.T) {
	t.Parallel()
	prog, choice := budgetGuardedChoiceProgram(t)
	c := newClosureWalker(&compile.Compiler{Prog: prog.Static()}, false)
	m := c.Expr(choice, compile.NewScope(), true)

	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		t.Run(unit.String()+"/budget", func(t *testing.T) {
			p := core.NewParser(&prog.sharedProgram, "z", unit, false)
			p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1})
			p.Silent = 1
			p.MaxDepth = budgetGuardedChoiceAlternatives + 1
			if n, ok := m(p); n != nil || ok || p.Control.Used != 1 {
				t.Fatalf("direct silent choice node=%v ok=%v spent=%d, want one charged peek and no charge for %d skipped alternatives", n, ok, p.Control.Used, budgetGuardedChoiceAlternatives)
			}
		})

		t.Run(unit.String()+"/cancel", func(t *testing.T) {
			p := core.NewParser(&prog.sharedProgram, "z", unit, false)
			ctx := newPollingContext(1)
			p.Control = core.NewBudgetControl(ctx, limits.Limits{})
			p.Silent = 1
			p.MaxDepth = budgetGuardedChoiceAlternatives + 1
			if err := catchFatal(t, func() { _, _ = m(p) }); err != nil || p.Control.Used != 1 {
				t.Fatalf("direct silent choice err=%v steps=%d, want no poll between the charges of straight-line code", err, p.Control.Used)
			}
		})
	}
}

func TestBudgetGuardedChoiceExactSkipBudget(t *testing.T) {
	t.Parallel()
	prog, choice := budgetGuardedChoiceProgram(t)
	c := newClosureWalker(&compile.Compiler{Prog: prog.Static()}, false)
	m := c.Expr(choice, compile.NewScope(), false)
	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		for _, text := range []string{"z", ""} {
			for _, silent := range []bool{true, false} {
				t.Run(unit.String()+"/input="+strconv.Quote(text)+"/silent="+strconv.FormatBool(silent), func(t *testing.T) {
					p := core.NewParser(&prog.sharedProgram, text, unit, false)
					p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1})
					p.MaxDepth = budgetGuardedChoiceAlternatives + 1
					if silent {
						p.Silent = 1
					} else {
						// An older failure position also suppresses expectations.
						p.Farthest = 1
					}
					if n, ok := m(p); n != nil || ok {
						t.Fatalf("rejected choice returned %v, %v", n, ok)
					}
					// The peek is the choice's only charge, whatever the number of alternatives skipped.
					if p.Control.Used != 1 || len(p.Exp) != 0 || p.Pos != 0 {
						t.Fatalf("choice state spent=%d expectations=%v pos=%d", p.Control.Used, p.Exp, p.Pos)
					}
				})
			}
		}
	}
}

func TestBudgetGuardedChoicePassingMatcherCharge(t *testing.T) {
	t.Parallel()
	prog, _ := budgetGuardedChoiceProgram(t)
	for _, unit := range []PositionUnit{Bytes, CodePoints} {
		for _, skip := range []bool{false, true} {
			t.Run(unit.String()+"/skip="+strconv.FormatBool(skip), func(t *testing.T) {
				alts := []grammar.Expr{&grammar.Literal{Value: "z"}}
				steps := uint64(2) // one peek and the literal probe, one unit each; a skipped alternative is free
				if skip {
					alts = append([]grammar.Expr{&grammar.Literal{Value: "x"}}, alts...)
				}
				c := newClosureWalker(&compile.Compiler{Prog: prog.Static()}, false)
				m := c.Expr(&grammar.Choice{Alts: alts}, compile.NewScope(), false)
				p := core.NewParser(&prog.sharedProgram, "z", unit, false)
				p.Control = core.NewBudgetControl(nil, limits.Limits{Budget: steps})
				p.Silent = 1
				if n, ok := m(p); n != nil || !ok || p.Pos != 1 || p.Control.Used != steps {
					t.Fatalf("matching choice node=%v ok=%v pos=%d spent=%d", n, ok, p.Pos, p.Control.Used)
				}
			})
		}
	}
}

func TestBudgetGuardedChoicePublicParityAndFatalRetry(t *testing.T) {
	t.Parallel()
	prog, _ := budgetGuardedChoiceProgram(t)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				t.Run(backend.String()+"/"+unit.String()+"/recognize="+strconv.FormatBool(recognize), func(t *testing.T) {
					base := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize}
					for _, text := range []string{"z", "q", ""} {
						want, wantErr := prog.ParseWith("main", text, base)
						controlled := base
						controlled.Limits = limits.Limits{Budget: 1 << 20}
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
				budgetGuardedChoiceFatalAndRetry(t, prog, backend, unit, recognize, false)
				budgetGuardedChoiceFatalAndRetry(t, prog, backend, unit, recognize, true)
			}
		}
	}
}

func budgetGuardedChoiceFatalAndRetry(t *testing.T, prog *Program, backend Backend, unit PositionUnit, recognize, cancel bool) {
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
		snapshot := budgetGuardedChoiceSnapshot(retained)

		var observed *core.Parser
		fallbackEntries, exits := 0, 0
		opts := ParseOptions{
			Backend:   backend,
			Unit:      unit,
			Recognize: recognize,
			Limits:    limits.Limits{Budget: 1 << 30},
			Trace: func(e TraceEvent) {
				if e.Kind == TraceEnter && e.Rule == "main" {
					observed = core.TraceParser(e)
					if cancel {
						core.TraceParser(e).Control.Ctx = newPollingContext(2)
					} else {
						core.TraceParser(e).Control.Limits.Budget = core.TraceParser(e).Control.Used + 1
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
			if !errors.As(gotErr, &limit) || limit.Kind != limits.LimitBudget {
				t.Fatalf("budgeted parse error=%v, want spend LimitError", gotErr)
			}
		}
		if fallbackEntries != 0 || exits != 0 {
			t.Fatalf("fatal spend entered fallback %d times or emitted %d exits", fallbackEntries, exits)
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
