//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func TestWorkTextFallbackBudgetCore(t *testing.T) {
	t.Parallel()
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		in := core.Input{Unit: u, Bs: make([]byte, 8192), In: make([]rune, 256), Control: core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: 1})}
		end := len(in.Bs)
		if u == CodePoints {
			end = len(in.In)
		}
		if err := catchFatal(t, func() { _ = in.Text(0, end) }); !errors.Is(err, limits.ErrWorkLimit) {
			t.Fatalf("%v materialization error=%v, want work limit", u, err)
		}
	}
}

func TestWorkTextSizingOverflow(t *testing.T) {
	t.Parallel()
	maxInt := int(^uint(0) >> 1)
	if got := core.CheckedTextSize(maxInt-3, 3); got != maxInt {
		t.Fatalf("exact maximum=%d", got)
	}
	for _, args := range [][2]int{{maxInt, 1}, {maxInt - 2, 3}, {-1, 1}, {0, -1}} {
		if err := catchFatal(t, func() { core.CheckedTextSize(args[0], args[1]) }); err == nil {
			t.Fatalf("sizing %v did not fail", args)
		}
	}
}

func TestWorkTextBatchPredicateAndTrace(t *testing.T) {
	t.Parallel()
	source := strings.Repeat("é😀\xff", 80)
	for _, traceText := range []bool{false, true} {
		grammar := `rule main = words:piece {text($words) != ""} $$

rule piece = .*`
		if traceText {
			grammar = `rule main = piece $$

rule piece = .*`
		}
		prog := mustCompile(t, grammar)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, u := range []PositionUnit{Bytes, CodePoints} {
				retained, err := prog.ParseWith("main", strings.Repeat("oldλ\xff", 90), ParseOptions{Backend: b, Unit: u, WorkLimits: limits.WorkLimits{MaxInputWork: 1 << 50}})
				if err != nil || retained == nil {
					t.Fatalf("retained parse %v/%v: %v", b, u, err)
				}
				before, _ := json.Marshal(retained)
				for _, recognize := range []bool{false, true} {
					for _, cancel := range []bool{false, true} {
						if cancel && u == Bytes {
							continue
						}
						var observed *core.Parser
						armed := false
						n, err := prog.ParseWith("main", source, ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: limits.WorkLimits{MaxInputWork: 1 << 50}, Trace: func(e TraceEvent) {
							observed = core.TraceParser(e)
							if e.Kind != TraceExit || e.Rule != "piece" || !e.Matched {
								return
							}
							armed = true
							if cancel {
								core.TraceParser(e).Control.InputSinceCheck, core.TraceParser(e).Control.StepsSinceCheck = 0, 0
								polls := 2
								if traceText {
									polls = 1
								}
								core.TraceParser(e).Control.Ctx = newPollingContext(polls)
							} else {
								core.TraceParser(e).Control.Limits.MaxInputWork = core.TraceParser(e).Control.InputUsed + 16
							}
							if traceText {
								_ = e.Text()
							}
						}})
						if !armed || observed == nil || observed.Scratch != nil || observed.Control != nil {
							t.Fatalf("%v/%v trace=%t recognition=%t cancel=%t: trace/cleanup failed", b, u, traceText, recognize, cancel)
						}
						if u == CodePoints {
							target := limits.ErrWorkLimit
							if cancel {
								target = context.Canceled
							}
							if n != nil || !errors.Is(err, target) {
								t.Fatalf("%v/%v trace=%t recognition=%t cancel=%t: n=%v err=%v want %v", b, u, traceText, recognize, cancel, n, err, target)
							}
						} else if err != nil || (!recognize && n == nil) {
							t.Fatalf("source substring %v: n=%v err=%v", b, n, err)
						}
						n, err = prog.ParseWith("main", source, ParseOptions{Backend: b, Unit: u, Recognize: recognize})
						if err != nil || (!recognize && n == nil) || (recognize && n != nil) {
							t.Fatalf("%v/%v retry: n=%v err=%v", b, u, n, err)
						}
						after, _ := json.Marshal(retained)
						if string(after) != string(before) {
							t.Fatalf("%v/%v retained tree changed", b, u)
						}
					}
				}
			}
		}
	}
}

func TestWorkTextStepBudgetAndLazyOffsets(t *testing.T) {
	t.Parallel()
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		in := core.Input{Unit: u, Bs: make([]byte, 4097), In: make([]rune, 129), Control: core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 128})}
		end := len(in.In)
		if u == Bytes {
			end = len(in.Bs)
			in.Control.Limits.MaxSteps = 1
		}
		err := catchFatal(t, func() { _ = in.Text(0, end) })
		var limit *limits.WorkLimitError
		if !errors.As(err, &limit) || limit.Kind != limits.StepWorkLimit {
			t.Fatalf("%v step error=%v", u, err)
		}
	}
	source := "aé😀z"
	in := core.Input{Unit: CodePoints, Src: source, SrcOK: true, In: []rune(source), Control: core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: 1})}
	if err := catchFatal(t, func() { _ = in.Text(1, 3) }); !errors.Is(err, limits.ErrWorkLimit) || in.Offs != nil {
		t.Fatalf("lazy offsets error=%v offsets=%v", err, in.Offs)
	}
	in.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: 100})
	if got := in.Text(1, 3); got != "é😀" {
		t.Fatalf("offset retry=%q", got)
	}
}

func TestWorkTextNodeRoutesParity(t *testing.T) {
	t.Parallel()
	input := strings.Repeat("é\x00😀\xff", 40)
	for _, src := range []string{`rule main = .* #atomic $$`, `type Token = { Text string }

rule main Token = t:(.* $$) #atomic => Token{Text: text($t)}`, `rule main = item $$

rule item = "!" #recover(skip=.*)`} {
		prog := mustCompile(t, src)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, u := range []PositionUnit{Bytes, CodePoints} {
				for _, recognize := range []bool{false, true} {
					opts := ParseOptions{Backend: b, Unit: u, Recognize: recognize}
					want, wantErr := prog.ParseWith("main", input, opts)
					opts.WorkLimits = limits.WorkLimits{MaxInputWork: 1 << 50, MaxSteps: 1 << 50}
					got, gotErr := prog.ParseWith("main", input, opts)
					if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
						t.Fatalf("%v/%v recognition=%t node/error differs for %s", b, u, recognize, src)
					}
				}
			}
		}
	}
}
