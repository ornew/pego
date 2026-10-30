//go:build !pego_work_controls_reference

package engine

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
)

func fallbackTextInput(unit PositionUnit, base int, bytes []byte, runes []rune, control *core.WorkControl) core.Input {
	return core.Input{Unit: unit, Base: base, Bs: bytes, In: runes, Control: control}
}

func TestWorkInputTextNativeParityAndDetachedResult(t *testing.T) {
	t.Parallel()
	byteCases := []int{0, 128, 129, 4096, 4097}
	for _, n := range byteCases {
		source := make([]byte, n+6)
		for i := range source {
			source[i] = byte(i*37 + 11)
		}
		start, end := 3, n+3
		in := fallbackTextInput(Bytes, 17, source, nil, core.NewWorkControl(nil, limits.Limits{MaxWork: 100_000}))
		got := in.Text(17+start, 17+end)
		want := string(source[start:end])
		if got != want {
			t.Fatalf("Bytes length %d text mismatch", n)
		}
		if in.Control.Used != uint64(n) {
			t.Fatalf("Bytes length %d work=%d, want %d", n, in.Control.Used, n)
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
		in := fallbackTextInput(CodePoints, 9, nil, runes, core.NewWorkControl(nil, limits.Limits{MaxWork: 100_000}))
		got := in.Text(9+start, 9+end)
		want := string(runes[start:end])
		if got != want {
			t.Fatalf("CodePoints length %d text = %q, want %q", n, got, want)
		}
		if in.Control.Used != uint64(4*n) {
			t.Fatalf("CodePoints length %d work=%d, want %d", n, in.Control.Used, 4*n)
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
	t.Parallel()
	byteSource := []byte(strings.Repeat("a", 4097))
	byteBefore := append([]byte(nil), byteSource...)
	byteInput := fallbackTextInput(Bytes, 0, byteSource, nil, core.NewWorkControl(nil, limits.Limits{MaxWork: 4096}))
	if err := catchFatal(t, func() { _ = byteInput.Text(0, len(byteSource)) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("Bytes second-chunk error = %v, want ErrLimit", err)
	}
	if !reflect.DeepEqual(byteSource, byteBefore) || byteInput.Control.Used != 4096 {
		t.Fatalf("Bytes failed copy mutated source or charged unexpected work: counter=%d", byteInput.Control.Used)
	}
	byteInput.Control = core.NewWorkControl(nil, limits.Limits{MaxWork: 10_000})
	if got, want := byteInput.Text(0, len(byteSource)), string(byteSource); got != want {
		t.Fatal("Bytes retry differs from native conversion")
	}

	runeSource := make([]rune, 129)
	for i := range runeSource {
		runeSource[i] = 'λ'
	}
	runeBefore := append([]rune(nil), runeSource...)
	runeInput := fallbackTextInput(CodePoints, 0, nil, runeSource, core.NewWorkControl(nil, limits.Limits{MaxWork: 257}))
	if err := catchFatal(t, func() { _ = runeInput.Text(0, len(runeSource)) }); !errors.Is(err, limits.ErrLimit) {
		t.Fatalf("CodePoints encoding-chunk error = %v, want ErrLimit", err)
	}
	if !reflect.DeepEqual(runeSource, runeBefore) || runeInput.Control.Used != 256 {
		t.Fatalf("CodePoints failure mutated source or work counter=%d, want one completed chunk", runeInput.Control.Used)
	}
	runeInput.Control = core.NewWorkControl(nil, limits.Limits{MaxWork: 10_000})
	if got, want := runeInput.Text(0, len(runeSource)), string(runeSource); got != want {
		t.Fatal("CodePoints retry differs from native conversion")
	}
}

func TestWorkInputTextPollingCancellationAtCopyAndEncoding(t *testing.T) {
	t.Parallel()
	bytes := []byte(strings.Repeat("b", 8192))
	byteInput := fallbackTextInput(Bytes, 0, bytes, nil, core.NewWorkControl(newPollingContext(2), limits.Limits{}))
	if err := catchFatal(t, func() { _ = byteInput.Text(0, len(bytes)) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Bytes cancellation = %v, want context.Canceled", err)
	}
	if byteInput.Control.Used != 4096 {
		t.Fatalf("Bytes cancellation counter=%d, want one completed chunk", byteInput.Control.Used)
	}

	runes := make([]rune, 512)
	for i := range runes {
		runes[i] = '語'
	}
	runeInput := fallbackTextInput(CodePoints, 0, nil, runes, core.NewWorkControl(newPollingContext(3), limits.Limits{}))
	if err := catchFatal(t, func() { _ = runeInput.Text(0, len(runes)) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("CodePoints cancellation = %v, want context.Canceled", err)
	}
	if runeInput.Control.Used == 0 {
		t.Fatal("CodePoints cancellation did not occur after controlled work began")
	}
}

func TestWorkInputTextSourceSubstringRemainsConstantWork(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 8192)
	byteInput := core.Input{Unit: Bytes, Bs: []byte(long), Src: long, SrcOK: true, Control: core.NewWorkControl(nil, limits.Limits{MaxWork: 1})}
	if got := byteInput.Text(1024, 7168); got != long[1024:7168] {
		t.Fatal("Bytes source substring mismatch")
	}
	if byteInput.Control.Used != 0 {
		t.Fatalf("Bytes source substring charged work=%d", byteInput.Control.Used)
	}

	runes := []rune(long)
	offs := make([]int32, len(runes)+1)
	for i := range offs {
		offs[i] = int32(i)
	}
	codePointInput := core.Input{Unit: CodePoints, In: runes, Src: long, SrcOK: true, Offs: offs, Control: core.NewWorkControl(nil, limits.Limits{MaxWork: 1})}
	if got := codePointInput.Text(1024, 7168); got != long[1024:7168] {
		t.Fatal("CodePoints source substring mismatch")
	}
	if codePointInput.Control.Used != 0 {
		t.Fatalf("CodePoints source substring charged work=%d", codePointInput.Control.Used)
	}
}

func TestWorkTraceTextInterruptionRetriesPooledParser(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = .* $$`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{Bytes, CodePoints} {
			raw := strings.Repeat("z", 9000)
			if unit == CodePoints {
				raw = strings.Repeat("\xff", 9000)
			}
			armed := false
			var observed *core.Parser
			node, err := prog.ParseWith("main", raw, ParseOptions{
				Backend: backend,
				Unit:    unit,
				Limits: limits.Limits{
					MaxWork: 1 << 30,
				},
				Trace: func(e TraceEvent) {
					if !armed && e.Kind == TraceExit && e.Rule == "main" && e.Matched {
						armed = true
						observed = core.TraceParser(e)
						if unit == Bytes {
							core.TraceParser(e).SrcOK = false
						}
						core.TraceParser(e).Control.Limits.MaxWork = core.TraceParser(e).Control.Used
						_ = e.Text()
					}
				},
			})
			if !armed || node != nil || !errors.Is(err, limits.ErrLimit) {
				t.Fatalf("%v/%v trace Text interruption: armed=%t node=%v err=%v", backend, unit, armed, node, err)
			}
			if observed == nil || observed.Scratch != nil || observed.Control != nil {
				t.Fatalf("%v/%v interrupted parser cleanup: %#v", backend, unit, observed)
			}
			node, err = prog.ParseWith("main", raw, ParseOptions{Backend: backend, Unit: unit})
			if err != nil || node == nil {
				t.Fatalf("%v/%v pooled retry: node=%v err=%v", backend, unit, node, err)
			}
		}
	}
}
