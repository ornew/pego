package engine

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const frameScratchGrammar = `
def main = h:@"#head\n" items:record* #stream $$ [text($h) == "#head\n"]
def record = k:@(?a-z / "é" / "😀")+ "=" vs:value+ "\n"
def value = v:@(?0-9)+ ","`

// This complements bench.BenchmarkStream's 50,001-row CSV workload with
// Unicode input, a live root capture, both units, and small and larger streams.
func BenchmarkStreamFrameScratch(b *testing.B) {
	for _, size := range []int{32, 5000} {
		input := "#head\n" + strings.Repeat("keyé😀=42,21,\n", size)
		g, err := syntax.Parse(frameScratchGrammar)
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, Options{})
		if err != nil {
			b.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if _, err := prog.rule(backend, "main"); err != nil {
				b.Fatal(err)
			}
			for _, unit := range []Unit{CodePoints, Bytes} {
				b.Run(fmt.Sprintf("%d/%s/%s", size, backend, unit), func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						err := prog.ParseStreamWith("main", strings.NewReader(input), func(*Node) error {
							count++
							return nil
						}, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || count != size {
							b.Fatalf("emitted %d of %d: %v", count, size, err)
						}
					}
				})
			}
		}
	}
}

func TestStreamFrameScratchCompletionPaths(t *testing.T) {
	stop := errors.New("stop after retained element")
	for _, tc := range []struct {
		name, src, input string
		count            int
		fail, stop       bool
	}{
		{"failed final element", records, "#records\n" + strings.Repeat("key=42\n", 300) + "key=x\n", 300, true, false},
		{"callback error", records, "#records\n" + strings.Repeat("key=42\n", 320), 300, true, true},
		{"zero progress", `def main = item* #stream $$
def item = x:@"x" / e:@""`, strings.Repeat("x", 300), 301, false, false},
		{"no capture scopes", `def main = "x"* #stream $$`, strings.Repeat("x", 300), 300, false, false},
		{"empty input", `def main = "x"* #stream $$`, "", 0, false, false},
	} {
		prog := compile(t, tc.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				t.Run(fmt.Sprintf("%s/%s/%s", tc.name, backend, unit), func(t *testing.T) {
					var retained []*Node
					var snapshots [][]byte
					err := prog.ParseStreamWith("main", strings.NewReader(tc.input), func(n *Node) error {
						snapshot, err := n.MarshalJSON()
						if err != nil {
							return err
						}
						retained = append(retained, n)
						snapshots = append(snapshots, snapshot)
						if tc.stop && len(retained) == tc.count {
							return stop
						}
						return nil
					}, ParseOptions{Backend: backend, Unit: unit})
					if len(retained) != tc.count || (err != nil) != tc.fail || tc.stop && err != stop {
						t.Fatalf("emitted %d, error %v; want %d, failure %t", len(retained), err, tc.count, tc.fail)
					}
					for i, n := range retained {
						after, err := n.MarshalJSON()
						if err != nil || !bytes.Equal(after, snapshots[i]) {
							t.Fatalf("element %d changed after stream completion: %v", i, err)
						}
					}
				})
			}
		}
	}
}

// Retained results must remain independent of capture frames reused by later
// elements, including elements that exhaust more than one frame chunk.
func TestStreamFrameScratchRetainedResults(t *testing.T) {
	t.Run("CST", func(t *testing.T) {
		checkStreamFrameScratchRetainedResults(t, frameScratchGrammar)
	})
	t.Run("actions", func(t *testing.T) {
		checkStreamFrameScratchRetainedResults(t, `
type Value struct { N Match }
type Row struct { Key Match, Values []Value }
def main = h:@"#head\n" items:record* #stream $$ [text($h) == "#head\n"]
def record: Row = k:@(?a-z / "é" / "😀")+ "=" vs:value+ "\n" -> new Row{Key: $k, Values: $vs}
def value: Value = v:@(?0-9)+ "," -> new Value{N: $v}`)
	})
}

func checkStreamFrameScratchRetainedResults(t *testing.T, src string) {
	t.Helper()
	prog := compile(t, src)
	var input strings.Builder
	input.WriteString("#head\n")
	for i := range 320 {
		input.WriteString("keyé😀=")
		// One large element crosses both the pointer and capture-frame chunks.
		n := 1 + i%4
		if i == 160 {
			n = nodeChunk + 37
		}
		for j := range n {
			fmt.Fprintf(&input, "%d,", i+j)
		}
		input.WriteByte('\n')
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				whole, err := prog.ParseWith("main", input.String(), opts)
				if err != nil {
					t.Fatal(err)
				}
				want := whole.Field("items").(*Node).Children
				var retained []*Node
				var snapshots [][]byte
				err = prog.ParseStreamWith("main", strings.NewReader(input.String()), func(n *Node) error {
					snapshot, err := n.MarshalJSON()
					if err != nil {
						return err
					}
					retained = append(retained, n)
					snapshots = append(snapshots, snapshot)
					return nil
				}, opts)
				if err != nil || len(retained) != len(want) {
					t.Fatalf("retained %d of %d elements: %v", len(retained), len(want), err)
				}
				for i, n := range retained {
					after, err := n.MarshalJSON()
					if err != nil {
						t.Fatal(err)
					}
					expected, err := want[i].MarshalJSON()
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(after, snapshots[i]) || !bytes.Equal(after, expected) {
						t.Fatalf("element %d changed after a commit or differs from whole-input parsing", i)
					}
				}
			})
		}
	}
}

func TestStreamFrameScratchClearsDeadFrames(t *testing.T) {
	if !streamFrameScratch {
		t.Skip("reference build does not reuse capture frames")
	}
	p := newParser(compile(t, `def main = "a"`), "a", CodePoints, false)
	root := p.newFrame(1)
	root.vals[0] = &Node{Text: "live root"}
	p.frame = root
	dead := p.newFrame(1)
	dead.vals[0] = &Node{Text: "previous element"}
	p.resetStreamFrames()
	if root.vals[0].Text != "live root" || dead.vals != nil {
		t.Fatal("commit lost the live root or retained a dead capture frame")
	}
	if next := p.newFrame(1); next != dead || next.vals[0] != nil {
		t.Fatal("next element did not reuse the cleared frame with fresh child storage")
	}
	// The root can remain in an older chunk after a large element. The current
	// chunk is then entirely dead, while the older live root must survive.
	for range nodeChunk + 7 {
		p.newFrame(1).vals[0] = &Node{Text: "large element"}
	}
	p.resetStreamFrames()
	if len(p.frameSlab) != 0 || cap(p.frameSlab) != nodeChunk || root.vals[0].Text != "live root" {
		t.Fatal("large element prevented scratch reuse or damaged the older root")
	}
	for _, f := range p.frameSlab[:cap(p.frameSlab)] {
		if f.vals != nil {
			t.Fatal("dead frame retains the large element")
		}
	}
}
