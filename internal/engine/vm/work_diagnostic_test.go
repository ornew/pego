//go:build !pego_work_controls_reference

package vm

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func TestWorkDiagnosticRecoveredCopies(t *testing.T) {
	for _, name := range []string{"memo replay", "memo snapshot", "LR snapshot", "LR replay", "OperatorsRule snapshot", "iterative OperatorsRule snapshot", "OperatorsRule replay"} {
		for _, mode := range []string{"complete", "budget", "cancel"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				a := &core.SyntaxError{Messages: []string{"retained-a"}}
				b := &core.SyntaxError{Expected: []string{"retained-b"}}
				prefix := &core.SyntaxError{Messages: []string{"prefix"}}
				records := make([]*core.SyntaxError, 320)
				for i := range records {
					records[i] = []*core.SyntaxError{a, b, a}[i%3]
				}
				before, _ := json.Marshal(records)
				retained := value.Ptr(value.Data{Text: "retained", Fields: value.Fields{{Name: "keep", Value: "stable"}}})
				p := diagnosticParser(limits.WorkLimits{})
				p.Memo, p.Recovered = core.NewMemoTable(), []*core.SyntaxError{prefix}
				p.Memo.Slots = make([]*core.MemoEntry, 1)
				r := &core.Rule{ID: 7}
				key := core.MemoKey{Rule: 7}
				steps := uint64(len(records))
				var run, completed, aborted func()
				assertReplay := func() {
					if len(p.Recovered) != 321 || p.Recovered[0] != prefix || !slices.Equal(p.Recovered[1:], records) {
						t.Fatal("recovered replay changed pointer order or prefix")
					}
				}
				assertSnapshot := func(got []*core.SyntaxError) {
					if !slices.Equal(got, records) || &got[0] == &records[0] {
						t.Fatal("snapshot lost pointers or aliases the source slice")
					}
				}
				aborted = func() {}
				switch name {
				case "memo replay":
					p.Memo.Slots[0] = &core.MemoEntry{Rule: 7, Ok: true, End: 1, Node: retained, Errs: records}
					steps += 2 // lookup and entry visit, then the controlled replay
					var ok, hit bool
					var val *value.Node
					run = func() { _, val, ok, hit = p.CallBegin(r, 0) }
					completed = func() {
						assertReplay()
						if !ok || !hit || val != retained {
							t.Fatal("memo hit result differs")
						}
					}
				case "memo snapshot":
					p.Pos = 1
					p.Recovered = append(p.Recovered, records...)
					steps += 2 // atomic memo put after the snapshot
					run = func() { p.CallEnd(r, &core.CallState{Key: key, Rec: 1}, retained, true) }
					completed = func() { assertSnapshot(p.Memo.Slots[0].Errs) }
					aborted = func() {
						if p.Memo.Slots[0] != nil {
							t.Fatal("partial memo snapshot published")
						}
					}
				case "LR snapshot":
					seed := &core.MemoEntry{Rule: 7, Growing: true}
					p.Memo.Slots[0], p.ActiveSeed, p.Pos = seed, seed, 1
					p.Recovered = append(p.Recovered, records...)
					g := core.GrowState{Key: key, Rec: 1, Best: seed}
					steps += 4 // growth visit plus replacement put
					run = func() {
						if !p.GrowStep(&g, retained, true) {
							t.Fatal("LR seed did not grow")
						}
					}
					completed = func() {
						assertSnapshot(g.Best.Errs)
						if p.ActiveSeed != g.Best || p.Memo.Slots[0] != g.Best || len(p.Recovered) != 1 {
							t.Fatal("LR publication state differs")
						}
					}
					aborted = func() {
						if p.Memo.Slots[0] != seed || p.ActiveSeed != seed {
							t.Fatal("partial LR seed published")
						}
					}
				case "LR replay":
					g := core.GrowState{Rec: 1, Best: &core.MemoEntry{Ok: true, End: 1, Node: retained, Errs: records}}
					run = func() { p.GrowEnd(r, &g) }
					completed = assertReplay
				case "OperatorsRule snapshot", "iterative OperatorsRule snapshot":
					op := &core.OperatorsRuleOp{Kind: grammar.Prefix, Line: &core.OperatorsRuleLine{Scope: &compile.Scope{}}}
					var best core.OperatorsRuleAttempt
					if name == "OperatorsRule snapshot" {
						op.Line.M = func(p *core.Parser) (*value.Node, bool) {
							p.Pos = 1
							p.Recovered = append(p.Recovered, records...)
							return retained, true
						}
						run = func() { best, _ = p.Longest([]*core.OperatorsRuleOp{op}) }
					} else {
						p.Pos = 1
						p.Recovered = append(p.Recovered, records...)
						f := &longestFrame{started: true, ops: []*core.OperatorsRuleOp{op}, m0: core.Mark{Recovered: 1}, f: p.NewFrame(0)}
						run = func() { f.next(ivmMachineOf(p), nil, iresult{ok: true, v: retained}); best = ivmMachineOf(p).attempt }
					}
					completed = func() {
						assertSnapshot(best.Errs)
						if best.Op != op || best.V != retained || len(p.Recovered) != 1 {
							t.Fatal("OperatorsRule winner or rollback differs")
						}
					}
					aborted = func() {
						if best.Op != nil || ivmMachineOf(p).attempt.Op != nil {
							t.Fatal("partial OperatorsRule winner returned")
						}
					}
				case "OperatorsRule replay":
					run = func() { p.Apply(&core.OperatorsRuleAttempt{End: 1, Errs: records}) }
					completed = assertReplay
				}
				if mode == "complete" {
					p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: steps})
				} else if mode == "budget" {
					// Less than the copy itself, so failure cannot be at a later put.
					p.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 319})
				} else {
					p.Control = core.NewWorkControl(newPollingContext(1), limits.WorkLimits{})
				}
				err := catchFatal(t, run)
				if mode == "complete" {
					if err != nil || p.Control.StepsUsed != steps {
						t.Fatalf("copy error=%v steps=%d want%d", err, p.Control.StepsUsed, steps)
					}
					completed()
				} else {
					want := limits.ErrWorkLimit
					if mode == "cancel" {
						want = context.Canceled
					}
					if !errors.Is(err, want) {
						t.Fatalf("copy interruption=%v want%v", err, want)
					}
					aborted()
				}
				after, _ := json.Marshal(records)
				if string(before) != string(after) || records[0] != a || value.D(retained).Text != "retained" || value.D(retained).Fields[0].Value != "stable" {
					t.Fatal("copy changed retained errors, alias pointers or Node")
				}
			})
		}
	}
}
