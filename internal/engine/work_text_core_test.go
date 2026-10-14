//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestWorkTextFallbackBudgetCore(t *testing.T) {
	for _, u := range []Unit{Bytes, CodePoints} {
		in := input{unit: u, bs: make([]byte, 8192), in: make([]rune, 256), control: newWorkControl(nil, WorkLimits{MaxInputWork: 1})}
		end := len(in.bs)
		if u == CodePoints {
			end = len(in.in)
		}
		if err := catchFatal(t, func() { _ = in.text(0, end) }); !errors.Is(err, ErrWorkLimit) {
			t.Fatalf("%v materialization error=%v, want work limit", u, err)
		}
	}
}

func TestWorkTextSizingOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := checkedTextSize(maxInt-3, 3); got != maxInt {
		t.Fatalf("exact maximum=%d", got)
	}
	for _, args := range [][2]int{{maxInt, 1}, {maxInt - 2, 3}, {-1, 1}, {0, -1}} {
		if err := catchFatal(t, func() { checkedTextSize(args[0], args[1]) }); err == nil {
			t.Fatalf("sizing %v did not fail", args)
		}
	}
}

func TestWorkTextBatchPredicateAndTrace(t *testing.T) {
	source := strings.Repeat("é😀\xff", 80)
	for _, traceText := range []bool{false, true} {
		grammar := `rule main = words:piece {text($words) != ""} $$

rule piece = .*`
		if traceText {
			grammar = `rule main = piece $$

rule piece = .*`
		}
		prog := compile(t, grammar)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, u := range []Unit{Bytes, CodePoints} {
				retained, err := prog.ParseWith("main", strings.Repeat("oldλ\xff", 90), ParseOptions{Backend: b, Unit: u, WorkLimits: WorkLimits{MaxInputWork: 1 << 50}})
				if err != nil || retained == nil {
					t.Fatalf("retained parse %v/%v: %v", b, u, err)
				}
				before, _ := json.Marshal(retained)
				for _, recognize := range []bool{false, true} {
					for _, cancel := range []bool{false, true} {
						if cancel && u == Bytes {
							continue
						}
						var observed *parser
						armed := false
						n, err := prog.ParseWith("main", source, ParseOptions{Backend: b, Unit: u, Recognize: recognize, WorkLimits: WorkLimits{MaxInputWork: 1 << 50}, Trace: func(e TraceEvent) {
							observed = e.p
							if e.Kind != TraceExit || e.Rule != "piece" || !e.Matched {
								return
							}
							armed = true
							if cancel {
								e.p.control.inputSinceCheck, e.p.control.stepsSinceCheck = 0, 0
								polls := 2
								if traceText {
									polls = 1
								}
								e.p.control.ctx = newPollingContext(polls)
							} else {
								e.p.control.limits.MaxInputWork = e.p.control.inputUsed + 16
							}
							if traceText {
								_ = e.Text()
							}
						}})
						if !armed || observed == nil || observed.scratch != nil || observed.control != nil {
							t.Fatalf("%v/%v trace=%t recognition=%t cancel=%t: trace/cleanup failed", b, u, traceText, recognize, cancel)
						}
						if u == CodePoints {
							target := ErrWorkLimit
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
	for _, u := range []Unit{Bytes, CodePoints} {
		in := input{unit: u, bs: make([]byte, 4097), in: make([]rune, 129), control: newWorkControl(nil, WorkLimits{MaxSteps: 128})}
		end := len(in.in)
		if u == Bytes {
			end = len(in.bs)
			in.control.limits.MaxSteps = 1
		}
		err := catchFatal(t, func() { _ = in.text(0, end) })
		var limit *WorkLimitError
		if !errors.As(err, &limit) || limit.Kind != StepWorkLimit {
			t.Fatalf("%v step error=%v", u, err)
		}
	}
	source := "aé😀z"
	in := input{unit: CodePoints, src: source, srcOK: true, in: []rune(source), control: newWorkControl(nil, WorkLimits{MaxInputWork: 1})}
	if err := catchFatal(t, func() { _ = in.text(1, 3) }); !errors.Is(err, ErrWorkLimit) || in.offs != nil {
		t.Fatalf("lazy offsets error=%v offsets=%v", err, in.offs)
	}
	in.control = newWorkControl(nil, WorkLimits{MaxInputWork: 100})
	if got := in.text(1, 3); got != "é😀" {
		t.Fatalf("offset retry=%q", got)
	}
}

func TestWorkTextNodeRoutesParity(t *testing.T) {
	input := strings.Repeat("é\x00😀\xff", 40)
	for _, src := range []string{`rule main = .* #atomic $$`, `type Token terminal

rule main Token = .* $$`, `rule main = item $$

rule item = "!" #recover(skip=.*)`} {
		prog := compile(t, src)
		for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, u := range []Unit{Bytes, CodePoints} {
				for _, recognize := range []bool{false, true} {
					opts := ParseOptions{Backend: b, Unit: u, Recognize: recognize}
					want, wantErr := prog.ParseWith("main", input, opts)
					opts.WorkLimits = WorkLimits{MaxInputWork: 1 << 50, MaxSteps: 1 << 50}
					got, gotErr := prog.ParseWith("main", input, opts)
					if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotErr, wantErr) {
						t.Fatalf("%v/%v recognition=%t node/error differs for %s", b, u, recognize, src)
					}
				}
			}
		}
	}
}
