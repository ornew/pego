//go:build !pego_budget_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

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

func TestBudgetDiagnosticBatchFailureAndTraceAccessor(t *testing.T) {
	t.Parallel()
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
						var c *core.BudgetControl
						var at uint64
						n, err := active.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Recognize: recognize, Limits: limits.Limits{Budget: 1 << 50}, Trace: func(e TraceEvent) {
							observed = core.TraceParser(e)
							if e.Kind == TraceEnter && e.Rule == "fallback" {
								fallback++
							}
							if !armed && e.Kind == TraceExit && ((accessor && e.Rule == "bad") || (!accessor && e.Rule == "main")) {
								if e.Matched || len(core.TraceExpectations(e)) != 16 {
									t.Fatalf("unexpected diagnostic fixture exp=%d", len(core.TraceExpectations(e)))
								}
								armed, c, at = true, core.TraceParser(e).Control, core.TraceParser(e).Control.Used
								if accessor {
									if cancel {
										c.Ctx = newPollingContext(1)
										c.SinceCheck = 0
									} else {
										c.Limits.Budget = at + 1
									}
									_ = e.Failure()
									accessorReturned = true
								} else {
									// More than every possible merge collision and descriptor visit;
									// long-string sorting must perform the eventual interruption.
									c.Limits.Budget = at + 4096
								}
							}
						}})
						var want error = limits.ErrLimit
						if cancel {
							want = context.Canceled
						}
						if !armed || n != nil || !errors.Is(err, want) || accessorReturned || fallback != 0 || observed == nil || observed.Scratch != nil || observed.Control != nil {
							t.Fatalf("%v/%v recognize=%t accessor=%t cancel=%t: armed=%t node=%t err=%v returned=%t fallback=%d", b, u, recognize, accessor, cancel, armed, n != nil, err, accessorReturned, fallback)
						}
						if !accessor && c.Used <= at+16*17 {
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

func TestBudgetDiagnosticRecoveryFatalAndRetainedOutputs(t *testing.T) {
	t.Parallel()
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
				n, err := prog.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Recognize: recognize, Limits: limits.Limits{Budget: 1 << 50}, Trace: func(e TraceEvent) {
					observed = core.TraceParser(e)
					if e.Kind == TraceEnter && e.Rule == "fallback" {
						fallback++
					}
					if !armed && e.Kind == TraceExit && e.Rule == "bad" {
						armed = true
						core.TraceParser(e).Control.Limits.Budget = core.TraceParser(e).Control.Used + 256
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
				} else if !errors.Is(err, limits.ErrLimit) || fallback != 0 {
					t.Fatalf("message interruption entered alternative: %v fallback=%d", err, fallback)
				}
				if !recognize {
					armed, fallback = false, 0
					n, err = prog.ParseWith("main", "x", ParseOptions{Backend: b, Unit: u, Limits: limits.Limits{Budget: 1 << 50}, Trace: func(e TraceEvent) {
						observed = core.TraceParser(e)
						if e.Kind == TraceEnter && e.Rule == "fallback" {
							fallback++
						}
						if !armed && e.Kind == TraceExit && e.Rule == "bad" {
							armed = true
							// The post-callback check succeeds; the message copy polls next.
							core.TraceParser(e).Control.Ctx = newPollingContext(2)
							core.TraceParser(e).Control.SinceCheck = 0
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
