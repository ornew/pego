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

	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/limits"
	"github.com/ornew/pego/internal/value"
)

func workStreamTextInput(u PositionUnit, text string) core.Input {
	in := core.Input{Unit: u, BaseLine: 1, BaseCol: 1, Reader: bufio.NewReader(strings.NewReader(""))}
	if u == Bytes {
		in.Bs = []byte(text)
	} else {
		in.In = []rune(text)
	}
	return in
}

func TestWorkStreamTextByteSnapshotBoundaries(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, size := range []int{63, 64, 1024, 1025, 4097} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			text := strings.Repeat("x", size)
			in := workStreamTextInput(Bytes, text)
			end := min(size, 1024)
			used := size
			if size >= 64 {
				used = min(size, core.StreamTextChunk)
			}
			in.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: uint64(used - 1)})
			before := in.StreamText
			var val string
			err := catchFatal(t, func() { val = in.Text(0, end) })
			var limit *limits.WorkLimitError
			if !errors.As(err, &limit) || limit.Kind != limits.InputWorkLimit || val != "" || !reflect.DeepEqual(in.StreamText, before) {
				t.Fatalf("materialization error=%v value=%q", err, val)
			}
			in.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: uint64(used)})
			if got := in.Text(0, end); got != text[:end] || in.Control.InputUsed != uint64(used) || in.Control.StepsUsed != 1 {
				t.Fatalf("exact materialization got=%q counters=%d/%d", got, in.Control.InputUsed, in.Control.StepsUsed)
			}
			if size < 64 && in.StreamText.Source != "" || size >= 64 && in.StreamText.End != used {
				t.Fatal("snapshot/fallback threshold changed")
			}
			if size >= 64 {
				input, steps := in.Control.InputUsed, in.Control.StepsUsed
				if in.Text(1, end) != text[1:end] || in.Control.InputUsed != input || in.Control.StepsUsed != steps {
					t.Fatal("cache hit performed new materialization")
				}
			}
		})
	}
}

func TestWorkStreamTextFallbackAndNoReadAhead(t *testing.T) {
	t.Parallel()
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		for _, size := range []int{1, 63, 1025, 4097} {
			t.Run(fmt.Sprintf("%s/%d", u, size), func(t *testing.T) {
				r := &snapshotOneByteReader{source: strings.Repeat("x", size+100)}
				in := core.Input{Unit: u, Reader: bufio.NewReader(r)}
				in.Fill(size)
				reads := r.calls
				in.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: 1 << 20})
				if in.Text(0, size) != strings.Repeat("x", size) || r.calls != reads {
					t.Fatal("controlled text read ahead or changed text")
				}
				if size < 64 || size > core.StreamTextChunk {
					if in.StreamText.Source != "" {
						t.Fatal("fallback published a snapshot")
					}
				}
				if size > 1 && in.Control.InputUsed == 0 {
					t.Fatal("stream text bypassed materialization accounting")
				}
			})
		}
	}
}

func TestWorkStreamTextCodePointPublication(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	const size = 1024
	text := strings.Repeat("a", size)
	old := core.StreamTextSnapshot{Base: -10, End: -7, Source: "old", Offsets: []int32{0, 1, 2, 3}}
	for _, phase := range []string{"sizing", "encoding", "offsets", "last-offset"} {
		t.Run(phase, func(t *testing.T) {
			in := workStreamTextInput(CodePoints, text)
			in.StreamText = old
			limit := uint64(127)
			switch phase {
			case "encoding":
				limit = size + 127
			case "offsets":
				limit = 2 * size
			case "last-offset":
				limit = 3*size - 1
			}
			in.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: limit})
			before := append([]int32(nil), old.Offsets...)
			err := catchFatal(t, func() { _ = in.Text(0, 8) })
			if !errors.Is(err, limits.ErrWorkLimit) || !reflect.DeepEqual(in.StreamText, old) || !reflect.DeepEqual(old.Offsets, before) {
				t.Fatalf("%s error=%v mutated old cache", phase, err)
			}
		})
	}
	in := workStreamTextInput(CodePoints, text)
	// Detached sizing/encoding each visit 1024 runes. The offset decoder
	// reserves four bytes except for its final three ASCII positions.
	in.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxSteps: 3 * size, MaxInputWork: 6*size - 6})
	if got := in.Text(0, 5); got != "aaaaa" || in.Control.StepsUsed != 3*size || in.Control.InputUsed != 6*size-6 || in.StreamText.End != size {
		t.Fatalf("exact counters=%d/%d text=%q", in.Control.StepsUsed, in.Control.InputUsed, got)
	}
	input, steps := in.Control.InputUsed, in.Control.StepsUsed
	if in.Text(9, 15) != "aaaaaa" || in.Control.InputUsed != input || in.Control.StepsUsed != steps {
		t.Fatal("cached code-point offsets were rebuilt")
	}
	oneShort := workStreamTextInput(CodePoints, text)
	oneShort.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: 6*size - 7})
	if err := catchFatal(t, func() { _ = oneShort.Text(0, 5) }); !errors.Is(err, limits.ErrWorkLimit) || oneShort.StreamText.Source != "" {
		t.Fatalf("one-short error=%v", err)
	}
}

func TestWorkStreamTextMalformedAndRetainedCache(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		in := workStreamTextInput(u, strings.Repeat("aé😀\xff\xc0z", 500))
		in.Base = int(^uint(0)>>1) - in.Loaded() - 10
		in.Control = core.NewWorkControl(nil, limits.WorkLimits{MaxInputWork: 1 << 30})
		first := in.Text(in.Base+1, in.Base+10)
		var want string
		if u == Bytes {
			want = string(in.Bs[1:10])
		} else {
			want = string(in.In[1:10])
		}
		if first != want {
			t.Fatalf("%s malformed text=%q want %q", u, first, want)
		}
		if got := in.Text(in.Base+1100, in.Base+1110); got == "" || first != want || in.StreamText.Base != in.Base+1100 {
			t.Fatal("new snapshot changed old immutable text or absolute positions")
		}
		if u == CodePoints {
			in.In[0], in.In[1], in.In[2] = -1, 0xd800, 0x110000
			in.StreamText = core.StreamTextSnapshot{}
			if got := in.Text(in.Base, in.Base+3); got != strings.Repeat(string(utf8.RuneError), 3) {
				t.Fatalf("invalid scalar replacement=%q", got)
			}
		}
	}
}

func TestWorkStreamTextOffsetCancellation(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	in := workStreamTextInput(CodePoints, strings.Repeat("a", 1024))
	old := core.StreamTextSnapshot{Base: -3, End: -1, Source: "ok", Offsets: []int32{0, 1, 2}}
	in.StreamText = old
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &workStreamTextPhaseContext{Context: ctx, observe: func() bool { return in.Control.StepsUsed >= 2048 }}
	c.cancel = cancel
	in.Control = core.NewWorkControl(c, limits.WorkLimits{})
	err := catchFatal(t, func() { _ = in.Text(0, 10) })
	if err != context.Canceled || in.Control.StepsUsed < 2048 || in.Control.StepsUsed >= 3072 || !reflect.DeepEqual(in.StreamText, old) {
		t.Fatalf("offset cancel error=%v steps=%d", err, in.Control.StepsUsed)
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
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = (!"\n" .)+ #atomic "\n"`)
	workStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, phase := range []string{"budget", "cancel"} {
			t.Run(phase, func(t *testing.T) {
				text := strings.Repeat("aé😀\xff", 350) + "\n" + strings.Repeat("b", 100) + "\n"
				o := base
				o.WorkLimits.MaxInputWork = 1 << 30
				var saved TraceEvent
				var previous core.StreamTextSnapshot
				armed := false
				emissions := 0
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				o.Trace = func(e TraceEvent) {
					if core.TraceParser(saved) == nil {
						saved = e
					}
					if !armed && e.Kind == TraceEnter && e.Rule == "row" {
						// The rule has not captured text yet. Fill loads input
						// explicitly; Text on the entry exercises materialization.
						p := core.TraceParser(e)
						p.Fill(p.Pos + 64)
						previous = p.StreamText
						armed = true
						if phase == "budget" {
							p.Control.Limits.MaxInputWork = p.Control.InputUsed + 1
						} else {
							cancel()
							p.Control.Ctx = ctx
						}
						_ = p.Text(p.Pos, p.Pos+64)
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
				assertTraceParserDetached(t, core.TraceParser(saved), false)
				if err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { return nil }, base); err != nil {
					t.Fatalf("fresh replay: %v", err)
				}
			})
		}
	})
}
