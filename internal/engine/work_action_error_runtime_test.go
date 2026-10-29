//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

// These public-API probes exercise large action error strings at the final
// rule wrapper and at an eagerly evaluated expression used by a predicate.
func TestWorkActionErrorPublicParityAndInterruption(t *testing.T) {
	longRule := strings.Repeat("r", 8192)
	longType := strings.Repeat("T", 8192)
	probes := []struct {
		name, start, grammar, input, wantError string
		fallback                               string
		captureAction                          bool
	}{
		{
			name:  "long final rule prefix",
			start: longRule,
			grammar: `type A = { N int }
rule ` + longRule + ` = a:"a" => A{N: 1 / (len($a) - 1)}`,
			input:     "a",
			wantError: "action in " + longRule + ": division by zero",
		},
		{
			name:  "long cast type",
			start: "main",
			grammar: `type ` + longType + ` = { N int }
rule main = x:"a" d:dyn => $d.(` + longType + `)
rule dyn _ = ds / di
rule ds = "b" => "s"
rule di = _ => 1`,
			input:     "a",
			wantError: "action in main: cannot cast int to " + longType + " at input position 0 (line 1, column 1); cast at grammar position 2:31",
		},
		{
			name:  "discarded predicate error skips to fallback",
			start: "main",
			grammar: `type ` + longType + ` = { N int }
rule main = (d:dyn {$d.(` + longType + `) != nil} "a") / fallback
rule dyn _ = ds / di
rule ds = "b" => "s"
rule di = _ => 1
rule fallback = "a"`,
			input:    "a",
			fallback: "fallback",
		},
		{
			name:  "recognition evaluates captured action needed by predicate",
			start: "main",
			grammar: `type A = { N int }
rule main = x:` + longRule + ` {$x.N == 1} $$
rule ` + longRule + ` = a:"a" => A{N: 1 / (len($a) - 1)}`,
			input:         "a",
			wantError:     "action in " + longRule + ": division by zero",
			captureAction: true,
		},
	}

	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			prog := mustCompile(t, probe.grammar)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				for _, unit := range []PositionUnit{Bytes, CodePoints} {
					recognizeModes := []bool{false}
					if probe.captureAction {
						recognizeModes = []bool{false, true}
					}
					for _, recognize := range recognizeModes {
						base := ParseOptions{Backend: backend, Unit: unit}
						base.Recognize = recognize
						baseFallback := 0
						base.Trace = func(e TraceEvent) {
							if e.Kind == TraceEnter && e.Rule == probe.fallback {
								baseFallback++
							}
						}
						wantNode, wantErr := prog.ParseWith(probe.start, probe.input, base)
						limited := base
						limited.WorkLimits = limits.WorkLimits{MaxSteps: 1 << 50}
						gotNode, gotErr := prog.ParseWith(probe.start, probe.input, limited)
						if (gotNode == nil) != (wantNode == nil) || actionErrorNodeText(gotNode) != actionErrorNodeText(wantNode) || actionErrorMessage(gotErr) != actionErrorMessage(wantErr) {
							t.Fatalf("%v/%v generous limits changed result: node=%v err=%v; want node=%v err=%v", backend, unit, gotNode, gotErr, wantNode, wantErr)
						}
						if probe.wantError != "" && actionErrorMessage(wantErr) != probe.wantError {
							t.Fatalf("%v/%v recognize=%t native error = %q, want %q", backend, unit, recognize, actionErrorMessage(wantErr), probe.wantError)
						}
						if probe.fallback != "" && (wantNode == nil || wantErr != nil || baseFallback == 0) {
							t.Fatalf("%v/%v predicate did not take fallback: node=%v err=%v entries=%d", backend, unit, wantNode, wantErr, baseFallback)
						}

						for _, mode := range []string{"budget", "cancel"} {
							enteredFallback := 0
							opts := ParseOptions{Backend: backend, Unit: unit, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxSteps: 1 << 50}}
							if mode == "cancel" {
								opts.Context = context.Background()
							}
							armed := false
							opts.Trace = func(e TraceEvent) {
								if e.Kind == TraceEnter && e.Rule == probe.fallback {
									enteredFallback++
								}
								if !armed && e.Kind == TraceEnter && e.Rule == probe.start {
									armed = true
									if mode == "budget" {
										// Public smoke coverage for fatal propagation. Exact formatter-phase
										// exhaustion is covered by focused builder whiteboxes.
										core.TraceParser(e).Control.Limits.MaxSteps = core.TraceParser(e).Control.StepsUsed + 256
									} else {
										poll := newPollingContext(2)
										core.TraceParser(e).Control.Ctx = poll
										core.TraceParser(e).Control.StepsSinceCheck = 0
										core.TraceParser(e).Control.InputSinceCheck = 0
									}
								}
							}
							n, err := prog.ParseWith(probe.start, probe.input, opts)
							if !armed || n != nil {
								t.Fatalf("%v/%v/%s: arm=%t node=%v err=%v", backend, unit, mode, armed, n, err)
							}
							if mode == "budget" {
								var limit *limits.WorkLimitError
								if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit {
									t.Fatalf("%v/%v budget interruption=%v, want StepWorkLimit", backend, unit, err)
								}
							}
							if mode == "cancel" && !errors.Is(err, context.Canceled) {
								t.Fatalf("%v/%v cancellation=%v, want context.Canceled", backend, unit, err)
							}
							if enteredFallback != 0 {
								t.Fatalf("%v/%v/%s: fatal formatting entered fallback %d times", backend, unit, mode, enteredFallback)
							}

							retryNode, retryErr := prog.ParseWith(probe.start, probe.input, ParseOptions{Backend: backend, Unit: unit, Recognize: recognize})
							if (retryNode == nil) != (wantNode == nil) || actionErrorNodeText(retryNode) != actionErrorNodeText(wantNode) || actionErrorMessage(retryErr) != actionErrorMessage(wantErr) {
								t.Fatalf("%v/%v/%s retry changed result: node=%v err=%v; want node=%v err=%v", backend, unit, mode, retryNode, retryErr, wantNode, wantErr)
							}
						}
					}
				}
			}
		})
	}
}

func actionErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func actionErrorNodeText(node *value.Node) string {
	if node == nil {
		return ""
	}
	return node.String()
}
