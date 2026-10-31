package engine

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/compile"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

const frameScratchGrammar = `rule main = h:"#head\n" #atomic items:record* #stream $$ {text($h) == "#head\n"}
rule record = k:[a-z / "é" / "😀"]+ #atomic "=" vs:value+ "\n"
rule value = v:[0-9]+ #atomic ","`

// This complements bench.BenchmarkStream's 50,001-row CSV workload with
// Unicode input, a live root capture, both units, and small and larger streams.
func BenchmarkStreamFrameScratch(b *testing.B) {
	for _, size := range []int{32, 5000} {
		input := "#head\n" + strings.Repeat("keyé😀=42,21,\n", size)
		g, err := parseTest(frameScratchGrammar)
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, compile.Options{})
		if err != nil {
			b.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if _, err := prog.rule(backend, "main"); err != nil {
				b.Fatal(err)
			}
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				b.Run(fmt.Sprintf("%d/%s/%s", size, backend, unit), func(b *testing.B) {
					b.SetBytes(int64(len(input)))
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						err := prog.ParseStreamWith("main", strings.NewReader(input), func(*value.Node) error {
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
	t.Parallel()
	stop := errors.New("stop after retained element")
	for _, tc := range []struct {
		name, src, input string
		count            int
		fail, stop       bool
	}{
		{"failed final element", gentest.Records, "#records\n" + strings.Repeat("key=42\n", 300) + "key=x\n", 300, true, false},
		{"callback error", gentest.Records, "#records\n" + strings.Repeat("key=42\n", 320), 300, true, true},
		{"zero progress", `rule main = item* #stream $$
rule item = x:"x" #atomic / e:"" #atomic`, strings.Repeat("x", 300), 301, false, false},
		{"no capture scopes", `rule main = "x"* #stream $$`, strings.Repeat("x", 300), 300, false, false},
		{"empty input", `rule main = "x"* #stream $$`, "", 0, false, false},
	} {
		prog := mustCompile(t, tc.src)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				t.Run(fmt.Sprintf("%s/%s/%s", tc.name, backend, unit), func(t *testing.T) {
					var retained []*value.Node
					var snapshots [][]byte
					err := prog.ParseStreamWith("main", strings.NewReader(tc.input), func(n *value.Node) error {
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
	t.Parallel()
	t.Run("CST", func(t *testing.T) {
		checkStreamFrameScratchRetainedResults(t, frameScratchGrammar)
	})
	t.Run("actions", func(t *testing.T) {
		checkStreamFrameScratchRetainedResults(t, `type Value = { N node[string] }
type Row = { Key node[string]; Values []Value }
rule main = h:"#head\n" #atomic items:record* #stream $$ {text($h) == "#head\n"}
rule record Row = k:[a-z / "é" / "😀"]+ #atomic "=" vs:value+ "\n" => Row{Key: $k, Values: $vs.Value}
rule value Value = v:[0-9]+ #atomic "," => Value{N: $v}`)
	})
}

func checkStreamFrameScratchRetainedResults(t *testing.T, src string) {
	t.Helper()
	prog := mustCompile(t, src)
	var input strings.Builder
	input.WriteString("#head\n")
	for i := range 320 {
		input.WriteString("keyé😀=")
		// One large element crosses both the pointer and capture-frame chunks.
		n := 1 + i%4
		if i == 160 {
			n = core.NodeChunk + 37
		}
		for j := range n {
			fmt.Fprintf(&input, "%d,", i+j)
		}
		input.WriteByte('\n')
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				whole, err := prog.ParseWith("main", input.String(), opts)
				if err != nil {
					t.Fatal(err)
				}
				want := value.D(whole.Field("items").(*value.Node)).Children
				var retained []*value.Node
				var snapshots [][]byte
				err = prog.ParseStreamWith("main", strings.NewReader(input.String()), func(n *value.Node) error {
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
	t.Parallel()
	if !core.StreamFrameScratch {
		t.Skip("reference build does not reuse capture frames")
	}
	p := core.NewParser(&mustCompile(t, `rule main = "a"`).sharedProgram, "a", CodePoints, false)
	root := p.NewFrame(1)
	root.Vals[0] = value.Ptr(value.Data{Text: "live root"})
	p.Frame = root
	dead := p.NewFrame(1)
	dead.Vals[0] = value.Ptr(value.Data{Text: "previous element"})
	p.ResetStreamFrames()
	if value.D(root.Vals[0]).Text != "live root" || dead.Vals != nil {
		t.Fatal("commit lost the live root or retained a dead capture frame")
	}
	if next := p.NewFrame(1); next != dead || next.Vals[0] != nil {
		t.Fatal("next element did not reuse the cleared frame with fresh child storage")
	}
	// The root can remain in an older chunk after a large element. The current
	// chunk is then entirely dead, while the older live root must survive.
	for range core.NodeChunk + 7 {
		p.NewFrame(1).Vals[0] = value.Ptr(value.Data{Text: "large element"})
	}
	p.ResetStreamFrames()
	if len(p.FrameSlab) != 0 || cap(p.FrameSlab) != core.NodeChunk || value.D(root.Vals[0]).Text != "live root" {
		t.Fatal("large element prevented scratch reuse or damaged the older root")
	}
	for _, f := range p.FrameSlab[:cap(p.FrameSlab)] {
		if f.Vals != nil {
			t.Fatal("dead frame retains the large element")
		}
	}
}
