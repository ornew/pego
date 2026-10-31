//go:build !zp_budget_controls_reference

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

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/limits"
	"github.com/ornew/zeroparse/internal/value"
)

func budgetStreamTextInput(u PositionUnit, text string) core.Input {
	in := core.Input{Unit: u, BaseLine: 1, BaseCol: 1, Reader: bufio.NewReader(strings.NewReader(""))}
	if u == Bytes {
		in.Bs = []byte(text)
	} else {
		in.In = []rune(text)
	}
	return in
}

func TestBudgetStreamTextByteSnapshotBoundaries(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, size := range []int{63, 64, 1024, 1025, 4097} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			text := strings.Repeat("x", size)
			in := budgetStreamTextInput(Bytes, text)
			end := min(size, 1024)
			used := size
			if size >= 64 {
				used = min(size, core.StreamTextChunk)
			}
			in.Control = core.NewBudgetControl(nil, limits.Limits{Budget: uint64(used - 1)})
			before := in.StreamText
			var val string
			err := catchFatal(t, func() { val = in.Text(0, end) })
			var limit *limits.LimitError
			if !errors.As(err, &limit) || limit.Kind != limits.LimitBudget || val != "" || !reflect.DeepEqual(in.StreamText, before) {
				t.Fatalf("materialization error=%v value=%q", err, val)
			}
			in.Control = core.NewBudgetControl(nil, limits.Limits{Budget: uint64(used)})
			if got := in.Text(0, end); got != text[:end] || in.Control.Used != uint64(used) {
				t.Fatalf("exact materialization got=%q spent=%d", got, in.Control.Used)
			}
			if size < 64 && in.StreamText.Source != "" || size >= 64 && in.StreamText.End != used {
				t.Fatal("snapshot/fallback threshold changed")
			}
			if size >= 64 {
				used := in.Control.Used
				if in.Text(1, end) != text[1:end] || in.Control.Used != used {
					t.Fatal("cache hit performed new materialization")
				}
			}
		})
	}
}

func TestBudgetStreamTextFallbackAndNoReadAhead(t *testing.T) {
	t.Parallel()
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		for _, size := range []int{1, 63, 1025, 4097} {
			t.Run(fmt.Sprintf("%s/%d", u, size), func(t *testing.T) {
				r := &snapshotOneByteReader{source: strings.Repeat("x", size+100)}
				in := core.Input{Unit: u, Reader: bufio.NewReader(r)}
				in.Fill(size)
				reads := r.calls
				in.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 20})
				if in.Text(0, size) != strings.Repeat("x", size) || r.calls != reads {
					t.Fatal("controlled text read ahead or changed text")
				}
				if size < 64 || size > core.StreamTextChunk {
					if in.StreamText.Source != "" {
						t.Fatal("fallback published a snapshot")
					}
				}
				if size > 1 && in.Control.Used == 0 {
					t.Fatal("stream text bypassed materialization accounting")
				}
			})
		}
	}
}

func TestBudgetStreamTextCodePointPublication(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	const size = 1024
	text := strings.Repeat("a", size)
	old := core.StreamTextSnapshot{Base: -10, End: -7, Source: "old", Offsets: []int32{0, 1, 2, 3}}
	for _, phase := range []string{"sizing", "encoding", "offsets", "last-offset"} {
		t.Run(phase, func(t *testing.T) {
			in := budgetStreamTextInput(CodePoints, text)
			in.StreamText = old
			limit := uint64(255)
			switch phase {
			case "encoding":
				limit = 2*size + 255
			case "offsets":
				limit = 4 * size
			case "last-offset":
				limit = 5*size - 1
			}
			in.Control = core.NewBudgetControl(nil, limits.Limits{Budget: limit})
			before := append([]int32(nil), old.Offsets...)
			err := catchFatal(t, func() { _ = in.Text(0, 8) })
			if !errors.Is(err, limits.ErrLimit) || !reflect.DeepEqual(in.StreamText, old) || !reflect.DeepEqual(old.Offsets, before) {
				t.Fatalf("%s error=%v mutated old cache", phase, err)
			}
		})
	}
	in := budgetStreamTextInput(CodePoints, text)
	// Detached sizing and encoding each visit 1024 runes at two units per rune
	// (the rune is read and the text is built). The offset decoder reserves
	// the bytes of each character it decodes, one for an ASCII character.
	in.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 5 * size})
	if got := in.Text(0, 5); got != "aaaaa" || in.Control.Used != 5*size || in.StreamText.End != size {
		t.Fatalf("exact spent=%d text=%q", in.Control.Used, got)
	}
	used := in.Control.Used
	if in.Text(9, 15) != "aaaaaa" || in.Control.Used != used {
		t.Fatal("cached code-point offsets were rebuilt")
	}
	oneShort := budgetStreamTextInput(CodePoints, text)
	oneShort.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 5*size - 1})
	if err := catchFatal(t, func() { _ = oneShort.Text(0, 5) }); !errors.Is(err, limits.ErrLimit) || oneShort.StreamText.Source != "" {
		t.Fatalf("one-short error=%v", err)
	}
}

func TestBudgetStreamTextMalformedAndRetainedCache(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	for _, u := range []PositionUnit{Bytes, CodePoints} {
		in := budgetStreamTextInput(u, strings.Repeat("aé😀\xff\xc0z", 500))
		in.Base = int(^uint(0)>>1) - in.Loaded() - 10
		in.Control = core.NewBudgetControl(nil, limits.Limits{Budget: 1 << 30})
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

func TestBudgetStreamTextOffsetCancellation(t *testing.T) {
	t.Parallel()
	if !core.StreamTextSnapshots {
		t.Skip("snapshot path disabled")
	}
	in := budgetStreamTextInput(CodePoints, strings.Repeat("a", 1024))
	old := core.StreamTextSnapshot{Base: -3, End: -1, Source: "ok", Offsets: []int32{0, 1, 2}}
	in.StreamText = old
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &budgetStreamTextPhaseContext{Context: ctx, observe: func() bool { return in.Control.Used >= 2048 }}
	c.cancel = cancel
	in.Control = core.NewBudgetControl(c, limits.Limits{})
	err := catchFatal(t, func() { _ = in.Text(0, 10) })
	if err != context.Canceled || in.Control.Used < 2048 || in.Control.Used >= 3072 || !reflect.DeepEqual(in.StreamText, old) {
		t.Fatalf("offset cancel error=%v steps=%d", err, in.Control.Used)
	}
}

type budgetStreamTextPhaseContext struct {
	context.Context
	observe func() bool
	cancel  context.CancelFunc
}

func (c *budgetStreamTextPhaseContext) Err() error {
	if c.observe() {
		c.cancel()
	}
	return c.Context.Err()
}

func TestBudgetStreamTextPublicTraceInterruption(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = row* #stream $$

rule row = (!"\n" .)+ #atomic "\n"`)
	budgetStreamVariants(t, func(t *testing.T, base ParseOptions) {
		for _, phase := range []string{"budget", "cancel"} {
			t.Run(phase, func(t *testing.T) {
				text := strings.Repeat("aé😀\xff", 350) + "\n" + strings.Repeat("b", 100) + "\n"
				o := base
				o.Limits.Budget = 1 << 30
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
							p.Control.Limits.Budget = p.Control.Used + 1
						} else {
							cancel()
							p.Control.Ctx = ctx
						}
						_ = p.Text(p.Pos, p.Pos+64)
					}
				}
				err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { emissions++; return nil }, o)
				want := limits.ErrLimit
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
