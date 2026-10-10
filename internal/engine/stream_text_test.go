package engine

import (
	"bufio"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestStreamTextSnapshots(t *testing.T) {
	text := strings.Repeat("aé😀\xff\xc0z\n", 700)
	for _, unit := range []Unit{CodePoints, Bytes} {
		t.Run(unit.String(), func(t *testing.T) {
			in := input{unit: unit, reader: bufio.NewReader(strings.NewReader(text)), baseLine: 1, baseCol: 1}
			in.fill(1 << 20)
			n := in.loaded()
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
				got := in.text(start, end)
				if got != want(start, end) {
					t.Fatalf("[%d:%d]: %q", start, end, got)
				}
				retained = append(retained, got)
				expected = append(expected, want(start, end))
				if in.streamText.end-in.streamText.base > streamTextChunk || len(in.streamText.source) > 4*streamTextChunk {
					t.Fatal("snapshot exceeded its retention bound")
				}
			}
			in.discard(n)
			if in.streamText.source != "" {
				t.Fatal("discard retained snapshot source")
			}
			in.reader = bufio.NewReader(strings.NewReader(strings.Repeat("y", n)))
			in.eof = false
			in.fill(n + n)
			for i, got := range retained {
				if got != expected[i] {
					t.Fatalf("retained text %d changed", i)
				}
			}
		})
	}
}

func TestStreamTextSnapshotNearMaximumPosition(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		in := input{unit: unit, base: int(^uint(0)>>1) - 128, reader: bufio.NewReader(strings.NewReader(strings.Repeat("x", 100)))}
		in.fill(in.base + 100)
		if got := in.text(in.base, in.base+10); got != "xxxxxxxxxx" {
			t.Fatal(got)
		}
		if streamTextSnapshots && in.streamText.end != in.base+100 {
			t.Fatal("snapshot end overflowed")
		}
	}
}

func TestStreamTextSnapshotBoundaries(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		for _, size := range []int{63, 64, 1024, 1025, 3000} {
			t.Run(fmt.Sprintf("%s/%d", unit, size), func(t *testing.T) {
				var data strings.Builder
				for i := range size {
					data.WriteByte(byte('a' + i%26))
				}
				text := data.String()
				in := input{unit: unit, reader: bufio.NewReader(strings.NewReader(text)), baseLine: 1, baseCol: 1}
				in.fill(size)
				for _, r := range [][2]int{{0, 0}, {0, 1}, {0, min(1024, size)}, {0, min(1025, size)}, {min(1010, size), min(1040, size)}, {0, min(32, size)}, {size - 1, size}} {
					if got := in.text(r[0], r[1]); got != text[r[0]:r[1]] {
						t.Fatalf("range %v: %q", r, got)
					}
				}
				in.discard(size - 1)
				if got := in.text(0, size); got != text[size-1:] {
					t.Fatalf("clipped text: %q", got)
				}
			})
		}
	}
}

// Text extraction must never trigger a reader call just to fill a snapshot.
func TestStreamTextSnapshotDoesNotReadAhead(t *testing.T) {
	for _, unit := range []Unit{CodePoints, Bytes} {
		reader := &snapshotOneByteReader{source: strings.Repeat("x", 200)}
		in := input{unit: unit, reader: bufio.NewReader(reader)}
		in.fill(1)
		calls := reader.calls
		if got := in.text(0, 1); got != "x" || reader.calls != calls {
			t.Fatal("text read ahead")
		}
		if in.streamText.source != "" {
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
	prog := compile(t, `def main = rows:row* #stream $$
def row = x:@(!"\n" .)+ "\n"`)
	text := strings.Repeat("é😀a\xff\xc0z\n", 700)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				whole, err := prog.ParseWith("main", text, opts)
				if err != nil {
					t.Fatal(err)
				}
				want := whole.Field("rows").(*Node).Children
				var got []*Node
				err = prog.ParseStreamWith("main", strings.NewReader(text), func(n *Node) error { got = append(got, n); return nil }, opts)
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
	prog := compile(t, `def main = rows:row* #stream $$
def row = x:@(!"\n" .)+ "\n"`)
	text := strings.Repeat(strings.Repeat("é😀\xff\xc0a", 40)+"\n", 30)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				whole, err := prog.ParseWith("main", text, opts)
				if err != nil {
					t.Fatal(err)
				}
				want := whole.Field("rows").(*Node).Children
				var got []*Node
				err = prog.ParseStreamWith("main", &snapshotOneByteReader{source: text}, func(n *Node) error { got = append(got, n); return nil }, opts)
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
