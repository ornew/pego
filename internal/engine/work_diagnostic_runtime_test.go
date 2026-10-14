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
				retained := &Node{Text: "retained", Fields: Fields{{Name: "keep", Value: "stable"}}}
				p := diagnosticParser(WorkLimits{})
				p.memo, p.recovered = newMemoTable(), []*SyntaxError{prefix}
				p.memo.slots = make([]*memoEntry, 1)
				r := &rule{id: 7}
				key := memoKey{rule: 7}
				steps := uint64(len(records))
				var run, completed, aborted func()
				assertReplay := func() {
					if len(p.recovered) != 321 || p.recovered[0] != prefix || !slices.Equal(p.recovered[1:], records) {
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
					p.memo.slots[0] = &memoEntry{rule: 7, ok: true, end: 1, node: retained, errs: records}
					steps += 2 // lookup and entry visit, then the controlled replay
					var ok, hit bool
					var value *Node
					run = func() { _, value, ok, hit = p.callBegin(r, 0) }
					completed = func() {
						assertReplay()
						if !ok || !hit || value != retained {
							t.Fatal("memo hit result differs")
						}
					}
				case "memo snapshot":
					p.pos = 1
					p.recovered = append(p.recovered, records...)
					steps += 2 // atomic memo put after the snapshot
					run = func() { p.callEnd(r, &callState{key: key, rec: 1}, retained, true) }
					completed = func() { assertSnapshot(p.memo.slots[0].errs) }
					aborted = func() {
						if p.memo.slots[0] != nil {
							t.Fatal("partial memo snapshot published")
						}
					}
				case "LR snapshot":
					seed := &memoEntry{rule: 7, growing: true}
					p.memo.slots[0], p.activeSeed, p.pos = seed, seed, 1
					p.recovered = append(p.recovered, records...)
					g := growState{key: key, rec: 1, best: seed}
					steps += 4 // growth visit plus replacement put
					run = func() {
						if !p.growStep(&g, retained, true) {
							t.Fatal("LR seed did not grow")
						}
					}
					completed = func() {
						assertSnapshot(g.best.errs)
						if p.activeSeed != g.best || p.memo.slots[0] != g.best || len(p.recovered) != 1 {
							t.Fatal("LR publication state differs")
						}
					}
					aborted = func() {
						if p.memo.slots[0] != seed || p.activeSeed != seed {
							t.Fatal("partial LR seed published")
						}
					}
				case "LR replay":
					g := growState{rec: 1, best: &memoEntry{ok: true, end: 1, node: retained, errs: records}}
					run = func() { p.growEnd(r, &g) }
					completed = assertReplay
				case "OperatorsRule snapshot", "iterative OperatorsRule snapshot":
					op := &operatorsRuleOp{kind: grammar.Prefix, line: &operatorsRuleLine{scope: &scope{}}}
					var best operatorsRuleAttempt
					if name == "OperatorsRule snapshot" {
						op.line.m = func(p *parser) (*Node, bool) {
							p.pos = 1
							p.recovered = append(p.recovered, records...)
							return retained, true
						}
						run = func() { best, _ = p.longest([]*operatorsRuleOp{op}) }
					} else {
						p.pos = 1
						p.recovered = append(p.recovered, records...)
						f := &longestFrame{started: true, ops: []*operatorsRuleOp{op}, m0: mark{recovered: 1}, f: p.newFrame(0)}
						run = func() { f.next(p, nil, iresult{ok: true, v: retained}); best = p.attempt }
					}
					completed = func() {
						assertSnapshot(best.errs)
						if best.op != op || best.v != retained || len(p.recovered) != 1 {
							t.Fatal("OperatorsRule winner or rollback differs")
						}
					}
					aborted = func() {
						if best.op != nil || p.attempt.op != nil {
							t.Fatal("partial OperatorsRule winner returned")
						}
					}
				case "OperatorsRule replay":
					run = func() { p.apply(&operatorsRuleAttempt{end: 1, errs: records}) }
					completed = assertReplay
				}
				if mode == "complete" {
					p.control = newWorkControl(nil, WorkLimits{MaxSteps: steps})
				} else if mode == "budget" {
					// Less than the copy itself, so failure cannot be at a later put.
					p.control = newWorkControl(nil, WorkLimits{MaxSteps: 319})
				} else {
					p.control = newWorkControl(newPollingContext(1), WorkLimits{})
				}
				err := catchFatal(t, run)
				if mode == "complete" {
					if err != nil || p.control.stepsUsed != steps {
						t.Fatalf("copy error=%v steps=%d want%d", err, p.control.stepsUsed, steps)
					}
					completed()
				} else {
					want := ErrWorkLimit
					if mode == "cancel" {
						want = context.Canceled
					}
					if !errors.Is(err, want) {
						t.Fatalf("copy interruption=%v want%v", err, want)
					}
					aborted()
				}
				after, _ := json.Marshal(records)
				if string(before) != string(after) || records[0] != a || retained.Text != "retained" || retained.Fields[0].Value != "stable" {
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
	prog := compile(t, source)
	accessorProg := compile(t, strings.Replace(source, "rule main = bad $$\n", `rule main = (bad #recover(skip=.* #atomic) "!" / fallback) $$`+"\n", 1)+"\nrule fallback = .* #atomic\n")
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []Unit{Bytes, CodePoints} {
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
						var observed *parser
						var c *workControl
						var at uint64
						n, err := active.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
							observed = e.p
							if e.Kind == TraceEnter && e.Rule == "fallback" {
								fallback++
							}
							if !armed && e.Kind == TraceExit && ((accessor && e.Rule == "bad") || (!accessor && e.Rule == "main")) {
								if e.Matched || len(e.exp) != 16 {
									t.Fatalf("unexpected diagnostic fixture exp=%d", len(e.exp))
								}
								armed, c, at = true, e.p.control, e.p.control.stepsUsed
								if accessor {
									if cancel {
										c.ctx = newPollingContext(1)
										c.stepsSinceCheck, c.inputSinceCheck = 0, 0
									} else {
										c.limits.MaxSteps = at + 1
									}
									_ = e.Failure()
									accessorReturned = true
								} else {
									// More than every possible merge collision and descriptor visit;
									// long-string sorting must perform the eventual interruption.
									c.limits.MaxSteps = at + 4096
								}
							}
						}})
						var want error = ErrWorkLimit
						if cancel {
							want = context.Canceled
						}
						if !armed || n != nil || !errors.Is(err, want) || accessorReturned || fallback != 0 || observed == nil || observed.scratch != nil || observed.control != nil {
							t.Fatalf("%v/%v recognize=%t accessor=%t cancel=%t: armed=%t node=%t err=%v returned=%t fallback=%d", b, u, recognize, accessor, cancel, armed, n != nil, err, accessorReturned, fallback)
						}
						if !accessor && c.stepsUsed <= at+16*17 {
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
	prog := compile(t, src)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, u := range []Unit{Bytes, CodePoints} {
			retained, retainedErr := prog.ParseWith("main", "x!", ParseOptions{Backend: b, Unit: u})
			if retained == nil || retainedErr == nil {
				t.Fatal("recovery setup did not produce node and errors")
			}
			beforeNode, _ := json.Marshal(retained)
			beforeErr, _ := json.Marshal(retainedErr)
			for _, recognize := range []bool{false, true} {
				armed, fallback := false, 0
				var observed *parser
				n, err := prog.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
					observed = e.p
					if e.Kind == TraceEnter && e.Rule == "fallback" {
						fallback++
					}
					if !armed && e.Kind == TraceExit && e.Rule == "bad" {
						armed = true
						e.p.control.limits.MaxSteps = e.p.control.stepsUsed + 256
					}
				}})
				if !armed || n != nil || observed.scratch != nil || observed.control != nil {
					t.Fatalf("recovery interruption fixture/cleanup differs: armed=%t node=%t error=%v fallback=%d", armed, n != nil, err, fallback)
				}
				if recognize {
					// Recognition builds no Error node and does no recovery-message copy.
					if err != nil || fallback != 1 {
						t.Fatalf("recognition unnecessarily formats message: %v fallback=%d", err, fallback)
					}
				} else if !errors.Is(err, ErrWorkLimit) || fallback != 0 {
					t.Fatalf("message interruption entered alternative: %v fallback=%d", err, fallback)
				}
				if !recognize {
					armed, fallback = false, 0
					n, err = prog.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, WorkLimits: WorkLimits{MaxSteps: 1 << 50}, Trace: func(e TraceEvent) {
						observed = e.p
						if e.Kind == TraceEnter && e.Rule == "fallback" {
							fallback++
						}
						if !armed && e.Kind == TraceExit && e.Rule == "bad" {
							armed = true
							// The post-callback check succeeds; the message copy polls next.
							e.p.control.ctx = newPollingContext(2)
							e.p.control.stepsSinceCheck, e.p.control.inputSinceCheck = 0, 0
						}
					}})
					if !armed || n != nil || !errors.Is(err, context.Canceled) || fallback != 0 || observed.scratch != nil || observed.control != nil {
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
