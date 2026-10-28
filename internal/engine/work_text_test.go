//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/limits"
)

func fallbackTextInput(unit PositionUnit, base int, bytes []byte, runes []rune, control *workControl) input {
	return input{unit: unit, base: base, bs: bytes, in: runes, control: control}
}

func TestWorkInputTextNativeParityAndDetachedResult(t *testing.T) {
	byteCases := []int{0, 128, 129, 4096, 4097}
	for _, n := range byteCases {
		source := make([]byte, n+6)
		for i := range source {
			source[i] = byte(i*37 + 11)
		}
		start, end := 3, n+3
		in := fallbackTextInput(Bytes, 17, source, nil, newWorkControl(nil, limits.WorkLimits{MaxInputWork: 100_000, MaxSteps: 100_000}))
		got := in.text(17+start, 17+end)
		want := string(source[start:end])
		if got != want {
			t.Fatalf("Bytes length %d text mismatch", n)
		}
		if in.control.inputUsed != uint64(n) || in.control.stepsUsed != uint64((n+4095)/4096) {
			t.Fatalf("Bytes length %d work=(%d,%d), want (%d,%d)", n, in.control.inputUsed, in.control.stepsUsed, n, (n+4095)/4096)
		}
		if n > 0 {
			source[start] ^= 0xff
			if got != want {
				t.Fatalf("Bytes length %d result changed with source buffer", n)
			}
		}
	}

	for _, n := range []int{0, 1, 128, 129, 257} {
		runes := make([]rune, n+4)
		for i := range runes {
			switch i % 7 {
			case 0:
				runes[i] = 0
			case 1:
				runes[i] = 'é'
			case 2:
				runes[i] = '💡'
			case 3:
				runes[i] = -1
			case 4:
				runes[i] = 0xd800
			case 5:
				runes[i] = 0x110000
			default:
				runes[i] = 'x'
			}
		}
		start, end := 2, n+2
		in := fallbackTextInput(CodePoints, 9, nil, runes, newWorkControl(nil, limits.WorkLimits{MaxInputWork: 100_000, MaxSteps: 100_000}))
		got := in.text(9+start, 9+end)
		want := string(runes[start:end])
		if got != want {
			t.Fatalf("CodePoints length %d text = %q, want %q", n, got, want)
		}
		if in.control.inputUsed != uint64(2*n) || in.control.stepsUsed != uint64(2*n) {
			t.Fatalf("CodePoints length %d work=(%d,%d), want (%d,%d)", n, in.control.inputUsed, in.control.stepsUsed, 2*n, 2*n)
		}
		if n > 0 {
			runes[start] = 'z'
			if got != want {
				t.Fatalf("CodePoints length %d result changed with source buffer", n)
			}
		}
	}
}

func TestWorkInputTextChunkInterruptionLeavesSourceAndRetries(t *testing.T) {
	byteSource := []byte(strings.Repeat("a", 4097))
	byteBefore := append([]byte(nil), byteSource...)
	byteInput := fallbackTextInput(Bytes, 0, byteSource, nil, newWorkControl(nil, limits.WorkLimits{MaxInputWork: 4096}))
	if err := catchFatal(t, func() { _ = byteInput.text(0, len(byteSource)) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("Bytes second-chunk error = %v, want ErrWorkLimit", err)
	}
	if !reflect.DeepEqual(byteSource, byteBefore) || byteInput.control.inputUsed != 4096 || byteInput.control.stepsUsed != 1 {
		t.Fatalf("Bytes failed copy mutated source or charged unexpected work: counters=(%d,%d)", byteInput.control.inputUsed, byteInput.control.stepsUsed)
	}
	byteInput.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 10_000, MaxSteps: 10_000})
	if got, want := byteInput.text(0, len(byteSource)), string(byteSource); got != want {
		t.Fatal("Bytes retry differs from native conversion")
	}

	runeSource := make([]rune, 129)
	for i := range runeSource {
		runeSource[i] = 'λ'
	}
	runeBefore := append([]rune(nil), runeSource...)
	runeInput := fallbackTextInput(CodePoints, 0, nil, runeSource, newWorkControl(nil, limits.WorkLimits{MaxInputWork: 257}))
	if err := catchFatal(t, func() { _ = runeInput.text(0, len(runeSource)) }); !errors.Is(err, limits.ErrWorkLimit) {
		t.Fatalf("CodePoints encoding-chunk error = %v, want ErrWorkLimit", err)
	}
	if !reflect.DeepEqual(runeSource, runeBefore) || runeInput.control.inputUsed != 257 || runeInput.control.stepsUsed != 257 {
		t.Fatalf("CodePoints failure mutated source or work counters=(%d,%d)", runeInput.control.inputUsed, runeInput.control.stepsUsed)
	}
	runeInput.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 10_000, MaxSteps: 10_000})
	if got, want := runeInput.text(0, len(runeSource)), string(runeSource); got != want {
		t.Fatal("CodePoints retry differs from native conversion")
	}
}

func TestWorkInputTextPollingCancellationAtCopyAndEncoding(t *testing.T) {
	bytes := []byte(strings.Repeat("b", 8192))
	byteInput := fallbackTextInput(Bytes, 0, bytes, nil, newWorkControl(newPollingContext(2), limits.WorkLimits{}))
	if err := catchFatal(t, func() { _ = byteInput.text(0, len(bytes)) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Bytes cancellation = %v, want context.Canceled", err)
	}
	if byteInput.control.inputUsed != 4096 || byteInput.control.stepsUsed != 1 {
		t.Fatalf("Bytes cancellation counters=(%d,%d), want one completed chunk", byteInput.control.inputUsed, byteInput.control.stepsUsed)
	}

	runes := make([]rune, 512)
	for i := range runes {
		runes[i] = '語'
	}
	runeInput := fallbackTextInput(CodePoints, 0, nil, runes, newWorkControl(newPollingContext(3), limits.WorkLimits{}))
	if err := catchFatal(t, func() { _ = runeInput.text(0, len(runes)) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("CodePoints cancellation = %v, want context.Canceled", err)
	}
	if runeInput.control.inputUsed == 0 || runeInput.control.stepsUsed == 0 {
		t.Fatal("CodePoints cancellation did not occur after controlled work began")
	}
}

func TestWorkInputTextSourceSubstringRemainsConstantWork(t *testing.T) {
	long := strings.Repeat("x", 8192)
	byteInput := input{unit: Bytes, bs: []byte(long), src: long, srcOK: true, control: newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1, MaxSteps: 1})}
	if got := byteInput.text(1024, 7168); got != long[1024:7168] {
		t.Fatal("Bytes source substring mismatch")
	}
	if byteInput.control.inputUsed != 0 || byteInput.control.stepsUsed != 0 {
		t.Fatalf("Bytes source substring charged work=(%d,%d)", byteInput.control.inputUsed, byteInput.control.stepsUsed)
	}

	runes := []rune(long)
	offs := make([]int32, len(runes)+1)
	for i := range offs {
		offs[i] = int32(i)
	}
	codePointInput := input{unit: CodePoints, in: runes, src: long, srcOK: true, offs: offs, control: newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1, MaxSteps: 1})}
	if got := codePointInput.text(1024, 7168); got != long[1024:7168] {
		t.Fatal("CodePoints source substring mismatch")
	}
	if codePointInput.control.inputUsed != 0 || codePointInput.control.stepsUsed != 0 {
		t.Fatalf("CodePoints source substring charged work=(%d,%d)", codePointInput.control.inputUsed, codePointInput.control.stepsUsed)
	}
}

func TestWorkTraceTextInterruptionRetriesPooledParser(t *testing.T) {
	prog := compile(t, `rule main = .* $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			raw := strings.Repeat("z", 9000)
			if unit == CodePoints {
				raw = strings.Repeat("\xff", 9000)
			}
			armed := false
			var observed *parser
			node, err := prog.ParseWith("main", raw, ParseOptions{
				Backend: backend,
				Unit:    unit,
				WorkLimits: limits.WorkLimits{
					MaxInputWork: 1 << 30,
				},
				Trace: func(e TraceEvent) {
					if !armed && e.Kind == TraceExit && e.Rule == "main" && e.Matched {
						armed = true
						observed = e.p
						if unit == Bytes {
							e.p.srcOK = false
						}
						e.p.control.limits.MaxInputWork = e.p.control.inputUsed
						_ = e.Text()
					}
				},
			})
			if !armed || node != nil || !errors.Is(err, limits.ErrWorkLimit) {
				t.Fatalf("%v/%v trace Text interruption: armed=%t node=%v err=%v", backend, unit, armed, node, err)
			}
			if observed == nil || observed.scratch != nil || observed.control != nil {
				t.Fatalf("%v/%v interrupted parser cleanup: %#v", backend, unit, observed)
			}
			node, err = prog.ParseWith("main", raw, ParseOptions{Backend: backend, Unit: unit})
			if err != nil || node == nil {
				t.Fatalf("%v/%v pooled retry: node=%v err=%v", backend, unit, node, err)
			}
		}
	}
}
