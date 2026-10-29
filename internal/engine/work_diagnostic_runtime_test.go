//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
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
				a := &SyntaxError{Messages: []string{"retained-a"}}
				b := &SyntaxError{Expected: []string{"retained-b"}}
				prefix := &SyntaxError{Messages: []string{"prefix"}}
				records := make([]*SyntaxError, 320)
				for i := range records {
					records[i] = []*SyntaxError{a, b, a}[i%3]
				}
				before, _ := json.Marshal(records)
				retained := value.Ptr(value.Data{Text: "retained", Fields: value.Fields{{Name: "keep", Value: "stable"}}})
				p := diagnosticParser(limits.WorkLimits{})
				p.Memo, p.Recovered = core.NewMemoTable(), []*SyntaxError{prefix}
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
				assertSnapshot := func(got []*SyntaxError) {
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

func diagnosticAlternatives() string {
	var src strings.Builder
	src.WriteString("rule main = bad $$\nrule bad = ")
	for i := 0; i < 16; i++ {
		if i > 0 {
			src.WriteString(" / ")
		}
		src.WriteString(strconv.Quote(strings.Repeat("long_common_prefix_", 60) + strconv.Itoa(i)))
	}
	return src.String()
}

func TestWorkDiagnosticBatchFailureAndTraceAccessor(t *testing.T) {
	source := diagnosticAlternatives()
	prog := mustCompile(t, source)
	accessorProg := mustCompile(t, strings.Replace(source, "rule main = bad $$\n", `rule main = (bad #recover(skip=.* #atomic) "!" / fallback) $$`+"\n", 1)+"\nrule fallback = .* #atomic\n")
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{Bytes, CodePoints} {
			for _, recognize := range []bool{false, true} {
				for _, accessor := range []bool{false, true} {
					active := prog
					if accessor {
						active = accessorProg
					}
					for _, cancel := range []bool{false, true} {
						if cancel && !accessor {
							continue
						}
						armed, accessorReturned := false, false
						fallback := 0
						var observed *core.Parser
						var c *core.WorkControl
						var at uint64
						n, err := active.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
							observed = core.TraceParser(e)
							if e.Kind == TraceEnter && e.Rule == "fallback" {
								fallback++
							}
							if !armed && e.Kind == TraceExit && ((accessor && e.Rule == "bad") || (!accessor && e.Rule == "main")) {
								if e.Matched || len(core.TraceExpectations(e)) != 16 {
									t.Fatalf("unexpected diagnostic fixture exp=%d", len(core.TraceExpectations(e)))
								}
								armed, c, at = true, core.TraceParser(e).Control, core.TraceParser(e).Control.StepsUsed
								if accessor {
									if cancel {
										c.Ctx = newPollingContext(1)
										c.StepsSinceCheck, c.InputSinceCheck = 0, 0
									} else {
										c.Limits.MaxSteps = at + 1
									}
									_ = e.Failure()
									accessorReturned = true
								} else {
									// More than every possible merge collision and descriptor visit;
									// long-string sorting must perform the eventual interruption.
									c.Limits.MaxSteps = at + 4096
								}
							}
						}})
						var want error = limits.ErrWorkLimit
						if cancel {
							want = context.Canceled
						}
						if !armed || n != nil || !errors.Is(err, want) || accessorReturned || fallback != 0 || observed == nil || observed.Scratch != nil || observed.Control != nil {
							t.Fatalf("%v/%v recognize=%t accessor=%t cancel=%t: armed=%t node=%t err=%v returned=%t fallback=%d", b, u, recognize, accessor, cancel, armed, n != nil, err, accessorReturned, fallback)
						}
						if !accessor && c.StepsUsed <= at+16*17 {
							t.Fatal("budget exhausted before diagnostic sorting")
						}
						n, err = active.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Recognize: recognize})
						if accessor {
							if err != nil || (recognize && n != nil) || (!recognize && n == nil) {
								t.Fatalf("accessor fresh retry differs: %v", err)
							}
						} else {
							var syntax *SyntaxError
							if n != nil || !errors.As(err, &syntax) || len(syntax.Expected) != 16 {
								t.Fatalf("failure fresh retry differs: %v", err)
							}
						}
					}
				}
			}
		}
	}
}

func TestWorkDiagnosticRecoveryFatalAndRetainedOutputs(t *testing.T) {
	message := strings.Repeat("recovery_message_λ", 100)
	// Stop before the trailing exclamation mark, including during the retry.
	src := `rule main = (bad #recover(skip=(!"!" .)+) "!" / fallback) $$
rule fallback = .* #atomic
rule bad = "a" @error(message=` + strconv.Quote(message) + `)`
	prog := mustCompile(t, src)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []PositionUnit{Bytes, CodePoints} {
			retained, retainedErr := prog.ParseWith("main", "x!", ParseOptions{Backend: b, Unit: u})
			if retained == nil || retainedErr == nil {
				t.Fatal("recovery setup did not produce node and errors")
			}
			beforeNode, _ := json.Marshal(retained)
			beforeErr, _ := json.Marshal(retainedErr)
			for _, recognize := range []bool{false, true} {
				armed, fallback := false, 0
				var observed *core.Parser
				n, err := prog.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
					observed = core.TraceParser(e)
					if e.Kind == TraceEnter && e.Rule == "fallback" {
						fallback++
					}
					if !armed && e.Kind == TraceExit && e.Rule == "bad" {
						armed = true
						core.TraceParser(e).Control.Limits.MaxSteps = core.TraceParser(e).Control.StepsUsed + 256
					}
				}})
				if !armed || n != nil || observed.Scratch != nil || observed.Control != nil {
					t.Fatalf("recovery interruption fixture/cleanup differs: armed=%t node=%t error=%v fallback=%d", armed, n != nil, err, fallback)
				}
				if recognize {
					// Recognition builds no Error node and does no recovery-message copy.
					if err != nil || fallback != 1 {
						t.Fatalf("recognition unnecessarily formats message: %v fallback=%d", err, fallback)
					}
				} else if !errors.Is(err, limits.ErrWorkLimit) || fallback != 0 {
					t.Fatalf("message interruption entered alternative: %v fallback=%d", err, fallback)
				}
				if !recognize {
					armed, fallback = false, 0
					n, err = prog.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
						observed = core.TraceParser(e)
						if e.Kind == TraceEnter && e.Rule == "fallback" {
							fallback++
						}
						if !armed && e.Kind == TraceExit && e.Rule == "bad" {
							armed = true
							// The post-callback check succeeds; the message copy polls next.
							core.TraceParser(e).Control.Ctx = newPollingContext(2)
							core.TraceParser(e).Control.StepsSinceCheck, core.TraceParser(e).Control.InputSinceCheck = 0, 0
						}
					}})
					if !armed || n != nil || !errors.Is(err, context.Canceled) || fallback != 0 || observed.Scratch != nil || observed.Control != nil {
						t.Fatalf("recovery message cancellation entered alternative or retained scratch: %v fallback=%d", err, fallback)
					}
				}
				_, retryErr := prog.ParseWith("main", "x!", ParseOptions{Backend: b, Unit: u, Recognize: recognize})
				var recovered SyntaxErrors
				if !errors.As(retryErr, &recovered) || len(recovered) != 1 || !reflect.DeepEqual(recovered[0].Messages, []string{message}) {
					t.Fatalf("recovery retry differs: %v", retryErr)
				}
				if after, _ := json.Marshal(retained); string(after) != string(beforeNode) {
					t.Fatal("retry changed retained recovery Node")
				}
				if after, _ := json.Marshal(retainedErr); string(after) != string(beforeErr) {
					t.Fatal("retry changed retained recovery errors")
				}
			}
		}
	}
}
