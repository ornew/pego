package engine

import (
	"bufio"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/value"
)

func TestStreamTextSnapshots(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("aé😀\xff\xc0z\n", 700)
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		t.Run(unit.String(), func(t *testing.T) {
			in := core.Input{Unit: unit, Reader: bufio.NewReader(strings.NewReader(text)), BaseLine: 1, BaseCol: 1}
			in.Fill(1 << 20)
			n := in.Loaded()
			want := func(start, end int) string {
				if unit == Bytes {
					return text[start:end]
				}
				return string([]rune(text)[start:end])
			}
			var retained []string
			var expected []string
			rng := rand.New(rand.NewPCG(20261011, 16))
			for range 500 {
				start := rng.IntN(n + 1)
				end := start + rng.IntN(n-start+1)
				got := in.Text(start, end)
				if got != want(start, end) {
					t.Fatalf("[%d:%d]: %q", start, end, got)
				}
				retained = append(retained, got)
				expected = append(expected, want(start, end))
				if in.StreamText.End-in.StreamText.Base > core.StreamTextChunk || len(in.StreamText.Source) > 4*core.StreamTextChunk {
					t.Fatal("snapshot exceeded its retention bound")
				}
			}
			in.Discard(n)
			if in.StreamText.Source != "" {
				t.Fatal("discard retained snapshot source")
			}
			in.Reader = bufio.NewReader(strings.NewReader(strings.Repeat("y", n)))
			in.Eof = false
			in.Fill(n + n)
			for i, got := range retained {
				if got != expected[i] {
					t.Fatalf("retained text %d changed", i)
				}
			}
		})
	}
}

func TestStreamTextSnapshotNearMaximumPosition(t *testing.T) {
	t.Parallel()
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		in := core.Input{Unit: unit, Base: int(^uint(0)>>1) - 128, Reader: bufio.NewReader(strings.NewReader(strings.Repeat("x", 100)))}
		in.Fill(in.Base + 100)
		if got := in.Text(in.Base, in.Base+10); got != "xxxxxxxxxx" {
			t.Fatal(got)
		}
		if core.StreamTextSnapshots && in.StreamText.End != in.Base+100 {
			t.Fatal("snapshot end overflowed")
		}
	}
}

func TestStreamTextSnapshotBoundaries(t *testing.T) {
	t.Parallel()
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		for _, size := range []int{63, 64, 1024, 1025, 3000} {
			t.Run(fmt.Sprintf("%s/%d", unit, size), func(t *testing.T) {
				var data strings.Builder
				for i := range size {
					data.WriteByte(byte('a' + i%26))
				}
				text := data.String()
				in := core.Input{Unit: unit, Reader: bufio.NewReader(strings.NewReader(text)), BaseLine: 1, BaseCol: 1}
				in.Fill(size)
				for _, r := range [][2]int{{0, 0}, {0, 1}, {0, min(1024, size)}, {0, min(1025, size)}, {min(1010, size), min(1040, size)}, {0, min(32, size)}, {size - 1, size}} {
					if got := in.Text(r[0], r[1]); got != text[r[0]:r[1]] {
						t.Fatalf("range %v: %q", r, got)
					}
				}
				in.Discard(size - 1)
				if got := in.Text(0, size); got != text[size-1:] {
					t.Fatalf("clipped text: %q", got)
				}
			})
		}
	}
}

// Text extraction must never trigger a reader call just to fill a snapshot.
func TestStreamTextSnapshotDoesNotReadAhead(t *testing.T) {
	t.Parallel()
	for _, unit := range []PositionUnit{CodePoints, Bytes} {
		reader := &snapshotOneByteReader{source: strings.Repeat("x", 200)}
		in := core.Input{Unit: unit, Reader: bufio.NewReader(reader)}
		in.Fill(1)
		calls := reader.calls
		if got := in.Text(0, 1); got != "x" || reader.calls != calls {
			t.Fatal("text read ahead")
		}
		if in.StreamText.Source != "" {
			t.Fatal("tiny fragment allocated a snapshot")
		}
	}
}

type snapshotOneByteReader struct {
	source string
	calls  int
}

func (r *snapshotOneByteReader) Read(p []byte) (int, error) {
	r.calls++
	if len(r.source) == 0 {
		return 0, io.EOF
	}
	p[0] = r.source[0]
	r.source = r.source[1:]
	return 1, nil
}

func TestStreamTextSnapshotRetainedMalformedResults(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = rows:row* #stream $$
rule row = x:(!"\n" .)+ #atomic "\n"`)
	text := strings.Repeat("é😀a\xff\xc0z\n", 700)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				whole, err := prog.ParseWith("main", text, opts)
				if err != nil {
					t.Fatal(err)
				}
				want := value.D(whole.Field("rows").(*value.Node)).Children
				var got []*value.Node
				err = prog.ParseStreamWith("main", strings.NewReader(text), func(n *value.Node) error { got = append(got, n); return nil }, opts)
				if err != nil || len(got) != len(want) {
					t.Fatalf("rows=%d: %v", len(got), err)
				}
				for i, n := range got {
					a, _ := n.MarshalJSON()
					b, _ := want[i].MarshalJSON()
					if string(a) != string(b) {
						t.Fatalf("row %d differs", i)
					}
				}
			})
		}
	}
}

func TestStreamTextSnapshotSplitUTF8(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = rows:row* #stream $$
rule row = x:(!"\n" .)+ #atomic "\n"`)
	text := strings.Repeat(strings.Repeat("é😀\xff\xc0a", 40)+"\n", 30)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				whole, err := prog.ParseWith("main", text, opts)
				if err != nil {
					t.Fatal(err)
				}
				want := value.D(whole.Field("rows").(*value.Node)).Children
				var got []*value.Node
				err = prog.ParseStreamWith("main", &snapshotOneByteReader{source: text}, func(n *value.Node) error { got = append(got, n); return nil }, opts)
				if err != nil || len(got) != len(want) {
					t.Fatalf("rows=%d: %v", len(got), err)
				}
				for i, n := range got {
					a, _ := n.MarshalJSON()
					b, _ := want[i].MarshalJSON()
					if string(a) != string(b) {
						t.Fatalf("row %d differs", i)
					}
				}
			})
		}
	}
}
