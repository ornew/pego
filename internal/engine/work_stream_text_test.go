//go:build !pego_work_controls_reference

package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func workStreamTextInput(u PositionUnit, text string) input {
	in := input{unit: u, baseLine: 1, baseCol: 1, reader: bufio.NewReader(strings.NewReader(""))}
	if u == Bytes {
		in.bs = []byte(text)
	} else {
		in.in = []rune(text)
	}
	return in
}

func TestWorkStreamTextByteSnapshotBoundaries(t *testing.T) {
	if !streamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, size := range []int{63, 64, 1024, 1025, 4097} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			text := strings.Repeat("x", size)
			in := workStreamTextInput(Bytes, text)
			end := min(size, 1024)
			used := size
			if size >= 64 {
				used = min(size, streamTextChunk)
			}
			in.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: uint64(used - 1)})
			before := in.streamText
			var val string
			err := catchFatal(t, func() { val = in.text(0, end) })
			var limit *limits.WorkLimitError
			if !errors.As(err, &limit) || limit.Kind != limits.InputWorkLimit || val != "" || !reflect.DeepEqual(in.streamText, before) {
				t.Fatalf("materialization error=%v value=%q", err, val)
			}
			in.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: uint64(used)})
			if got := in.text(0, end); got != text[:end] || in.control.inputUsed != uint64(used) || in.control.stepsUsed != 1 {
				t.Fatalf("exact materialization got=%q counters=%d/%d", got, in.control.inputUsed, in.control.stepsUsed)
			}
			if size < 64 && in.streamText.source != "" || size >= 64 && in.streamText.end != used {
				t.Fatal("snapshot/fallback threshold changed")
			}
			if size >= 64 {
				input, steps := in.control.inputUsed, in.control.stepsUsed
				if in.text(1, end) != text[1:end] || in.control.inputUsed != input || in.control.stepsUsed != steps {
					t.Fatal("cache hit performed new materialization")
				}
			}
		})
	}
}

func TestWorkStreamTextFallbackAndNoReadAhead(t *testing.T) {
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		for _, size := range []int{1, 63, 1025, 4097} {
			t.Run(fmt.Sprintf("%s/%d", u, size), func(t *testing.T) {
				r := &snapshotOneByteReader{source: strings.Repeat("x", size+100)}
				in := input{unit: u, reader: bufio.NewReader(r)}
				in.fill(size)
				reads := r.calls
				in.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1 << 20})
				if in.text(0, size) != strings.Repeat("x", size) || r.calls != reads {
					t.Fatal("controlled text read ahead or changed text")
				}
				if size < 64 || size > streamTextChunk {
					if in.streamText.source != "" {
						t.Fatal("fallback published a snapshot")
					}
				}
				if size > 1 && in.control.inputUsed == 0 {
					t.Fatal("stream text bypassed materialization accounting")
				}
			})
		}
	}
}

func TestWorkStreamTextCodePointPublication(t *testing.T) {
	if !streamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	const size = 1024
	text := strings.Repeat("a", size)
	old := streamTextSnapshot{base: -10, end: -7, source: "old", offsets: []int32{0, 1, 2, 3}}
	for _, phase := range []string{"sizing", "encoding", "offsets", "last-offset"} {
		t.Run(phase, func(t *testing.T) {
			in := workStreamTextInput(CodePoints, text)
			in.streamText = old
			limit := uint64(127)
			switch phase {
			case "encoding":
				limit = size + 127
			case "offsets":
				limit = 2 * size
			case "last-offset":
				limit = 3*size - 1
			}
			in.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: limit})
			before := append([]int32(nil), old.offsets...)
			err := catchFatal(t, func() { _ = in.text(0, 8) })
			if !errors.Is(err, limits.ErrWorkLimit) || !reflect.DeepEqual(in.streamText, old) || !reflect.DeepEqual(old.offsets, before) {
				t.Fatalf("%s error=%v mutated old cache", phase, err)
			}
		})
	}
	in := workStreamTextInput(CodePoints, text)
	// Detached sizing/encoding each visit 1024 runes. The offset decoder
	// reserves four bytes except for its final three ASCII positions.
	in.control = newWorkControl(nil, limits.WorkLimits{MaxSteps: 3 * size, MaxInputWork: 6*size - 6})
	if got := in.text(0, 5); got != "aaaaa" || in.control.stepsUsed != 3*size || in.control.inputUsed != 6*size-6 || in.streamText.end != size {
		t.Fatalf("exact counters=%d/%d text=%q", in.control.stepsUsed, in.control.inputUsed, got)
	}
	input, steps := in.control.inputUsed, in.control.stepsUsed
	if in.text(9, 15) != "aaaaaa" || in.control.inputUsed != input || in.control.stepsUsed != steps {
		t.Fatal("cached code-point offsets were rebuilt")
	}
	oneShort := workStreamTextInput(CodePoints, text)
	oneShort.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 6*size - 7})
	if err := catchFatal(t, func() { _ = oneShort.text(0, 5) }); !errors.Is(err, limits.ErrWorkLimit) || oneShort.streamText.source != "" {
		t.Fatalf("one-short error=%v", err)
	}
}

func TestWorkStreamTextMalformedAndRetainedCache(t *testing.T) {
	if !streamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		in := workStreamTextInput(u, strings.Repeat("aé😀\xff\xc0z", 500))
		in.base = int(^uint(0)>>1) - in.loaded() - 10
		in.control = newWorkControl(nil, limits.WorkLimits{MaxInputWork: 1 << 30})
		first := in.text(in.base+1, in.base+10)
		var want string
		if u == Bytes {
			want = string(in.bs[1:10])
		} else {
			want = string(in.in[1:10])
		}
		if first != want {
			t.Fatalf("%s malformed text=%q want %q", u, first, want)
		}
		if got := in.text(in.base+1100, in.base+1110); got == "" || first != want || in.streamText.base != in.base+1100 {
			t.Fatal("new snapshot changed old immutable text or absolute positions")
		}
		if u == CodePoints {
			in.in[0], in.in[1], in.in[2] = -1, 0xd800, 0x110000
			in.streamText = streamTextSnapshot{}
			if got := in.text(in.base, in.base+3); got != strings.Repeat(string(utf8.RuneError), 3) {
				t.Fatalf("invalid scalar replacement=%q", got)
			}
		}
	}
}

func TestWorkStreamTextOffsetCancellation(t *testing.T) {
	if !streamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	in := workStreamTextInput(CodePoints, strings.Repeat("a", 1024))
	old := streamTextSnapshot{base: -3, end: -1, source: "ok", offsets: []int32{0, 1, 2}}
	in.streamText = old
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &workStreamTextPhaseContext{Context: ctx, observe: func() bool { return in.control.stepsUsed >= 2048 }}
	c.cancel = cancel
	in.control = newWorkControl(c, limits.WorkLimits{})
	err := catchFatal(t, func() { _ = in.text(0, 10) })
	if err != context.Canceled || in.control.stepsUsed < 2048 || in.control.stepsUsed >= 3072 || !reflect.DeepEqual(in.streamText, old) {
		t.Fatalf("offset cancel error=%v steps=%d", err, in.control.stepsUsed)
	}
}

type workStreamTextPhaseContext struct {
	context.Context
	observe func() bool
	cancel  context.CancelFunc
}

func (c *workStreamTextPhaseContext) Err() error {
	if c.observe() {
		c.cancel()
	}
	return c.Context.Err()
}

func TestWorkStreamTextPublicTraceInterruption(t *testing.T) {
	prog := compile(t, `rule main = row* #stream $$

rule row = (!"\n" .)+ #atomic "\n"`)
	workStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, phase := range []string{"budget", "cancel"} {
			t.Run(phase, func(t *testing.T) {
				text := strings.Repeat("aé😀\xff", 350) + "\n" + strings.Repeat("b", 100) + "\n"
				o := base
				o.WorkLimits.MaxInputWork = 1 << 30
				var saved TraceEvent
				var previous streamTextSnapshot
				armed := false
				emissions := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				o.Trace = func(e TraceEvent) {
					if saved.p == nil {
						saved = e
					}
					if !armed && e.Kind == TraceEnter && e.Rule == "row" {
						// The rule has not captured text yet. Fill loads input
						// explicitly; Text on the entry exercises materialization.
						p := e.p
						p.fill(p.pos + 64)
						previous = p.streamText
						armed = true
						if phase == "budget" {
							p.control.limits.MaxInputWork = p.control.inputUsed + 1
						} else {
							cancel()
							p.control.ctx = ctx
						}
						_ = p.text(p.pos, p.pos+64)
					}
				}
				err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { emissions++; return nil }, o)
				want := limits.ErrWorkLimit
				if phase == "cancel" {
					want = context.Canceled
				}
				if !armed || !errors.Is(err, want) || emissions != 0 {
					t.Fatalf("phase=%s armed=%t error=%v emits=%d old=%+v", phase, armed, err, emissions, previous)
				}
				assertTraceParserDetached(t, saved.p, false)
				if err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { return nil }, base); err != nil {
					t.Fatalf("fresh replay: %v", err)
				}
			})
		}
	})
}
