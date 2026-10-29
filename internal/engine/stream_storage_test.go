package engine

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/compile"
	"github.com/ornew/pego/internal/engine/core"
	"github.com/ornew/pego/internal/value"
)

const streamStorageGrammar = `rule main = h:"#head\n" #atomic row* #stream $$ {text($h) == "#head\n"}
rule row = k:"x" #atomic vs:value+ "\n"
rule value = v:[a-z]+ #atomic ","`

func TestStreamPrefixStorageIsolation(t *testing.T) {
	t.Parallel()
	if !core.StreamPrefixIsolation {
		t.Skip("reference build shares prefix storage")
	}
	p := core.NewStreamParser(&mustCompile(t, `rule main = "a"`).sharedProgram, strings.NewReader("a"), CodePoints)
	root := p.NewFrame(1)
	p.Frame = root
	prefix := p.NewNode(value.Data{Text: "prefix"})
	root.Vals[0] = prefix
	if p.NodeSlab != nil || p.PtrSlab != nil || p.FrameSlab != nil {
		t.Fatal("prefix wasted or shared a bulk allocation")
	}
	p.StartStreamChunks()
	next := p.NewNode(value.Data{Text: "element"})
	element := p.NewFrame(1)
	if next == prefix || &element.Vals[0] == &root.Vals[0] || element == root {
		t.Fatal("element allocation still shares live prefix chunks")
	}
	if p.Frame != root || root.Vals[0] != prefix || value.D(prefix).Text != "prefix" {
		t.Fatal("prefix isolation changed live root capture")
	}
}

// Large prefixes must switch to slabs instead of allocating every object.
func TestStreamPrefixAllocationBudget(t *testing.T) {
	t.Parallel()
	if !core.StreamPrefixIsolation {
		t.Skip("reference build uses slabs")
	}
	p := core.NewStreamParser(&mustCompile(t, `rule main = "a"`).sharedProgram, strings.NewReader("a"), CodePoints)
	for range core.NodeChunk/8 + 4 {
		p.NewNode(value.Data{Text: "prefix"})
	}
	if len(p.NodeSlab) == 0 || p.NodeChunks != 1 {
		t.Fatal("large prefix did not switch to bounded slab allocation")
	}
	p.StartStreamChunks()
	if p.NodeChunks != 0 || len(p.NodeSlab) != 0 {
		t.Fatal("stream boundary did not seal large prefix storage")
	}
	p.NewNode(value.Data{Text: "element"})
	if p.NodeChunks != 1 || len(p.NodeSlab) != core.NodeChunk-1 {
		t.Fatal("elements did not resume normal slab allocation")
	}
}

// Prefix captures and emitted values must survive isolation for both compact
// and wide repetition instructions, including bytecode loaded from a compiled grammar.
func TestStreamPrefixLoadedRepetitions(t *testing.T) {
	t.Parallel()
	bounds := []string{"#repeat(0)", "#repeat(2)", "*"}
	if strconv.IntSize == 64 {
		bounds = append(bounds, "#repeat(0, 4294967296)")
	}
	for _, bound := range bounds {
		text, count := "#head\nxx", 2
		if bound == "#repeat(0)" {
			text, count = "#head\n", 0
		}
		source := `rule main = h:"#head\n" #atomic (x:"x" #atomic)` + bound + ` #stream $$ {text($h) == "#head\n"}`
		prog := mustCompile(t, source)
		for _, mode := range valueModes {
			saved := saveLoad(t, prog, "main", mode)
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if backend == Closure {
					err := saved.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { return nil }, servedBy(mode, ParseOptions{Backend: backend}))
					wantClosureRefused(t, err)
					continue
				}
				for _, unit := range []PositionUnit{CodePoints, Bytes} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", bound, mode, backend, unit), func(t *testing.T) {
						var retained []*value.Node
						err := saved.ParseStreamWith("main", strings.NewReader(text), func(n *value.Node) error { retained = append(retained, n); return nil }, servedBy(mode, ParseOptions{Backend: backend, Unit: unit}))
						if err != nil || len(retained) != count {
							t.Fatalf("got %d rows: %v", len(retained), err)
						}
						for _, n := range retained {
							if value.D(n.Field("x").(*value.Node)).Text != "x" {
								t.Fatal("retained capture changed")
							}
						}
					})
				}
			}
		}
	}
}

func TestStreamPrefixMemoReuse(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = &header h:header (x:"x" #atomic)* #stream $$ {text($h) == "#head\n"}
rule header = "#head\n" #atomic`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				input := "#head\n" + strings.Repeat("x", 1300)
				count := 0
				err := prog.ParseStreamWith("main", strings.NewReader(input), func(n *value.Node) error {
					if value.D(n.Field("x").(*value.Node)).Text != "x" {
						t.Fatal("emitted capture changed")
					}
					count++
					return nil
				}, opts)
				if err != nil || count != 1300 {
					t.Fatalf("emitted %d: %v", count, err)
				}
				if err := prog.ParseStreamWith("main", strings.NewReader(input), nil, opts); err != nil {
					t.Fatalf("nil callback: %v", err)
				}
			})
		}
	}
}

func TestStreamPrefixLeftRecursiveRoot(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = h:(main "a" / "a" #atomic) "x"* #stream {text($h) != ""}`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				count := 0
				err := prog.ParseStreamWith("main", strings.NewReader("aa"), func(*value.Node) error { count++; return nil }, ParseOptions{Backend: backend, Unit: unit})
				if err != nil || count != 0 {
					t.Fatalf("LR root capture lost: count=%d, %v", count, err)
				}
			})
		}
	}
}

func TestStreamPrefixLeftRecursiveCaptureSurvivesCommit(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `rule main = h:head #atomic "x"* #stream {text($h) == "aa"} $$
rule head = head "a" / "a" #atomic`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				count := 0
				err := prog.ParseStreamWith("main", strings.NewReader("aaxx"), func(*value.Node) error { count++; return nil }, ParseOptions{Backend: backend, Unit: unit})
				if err != nil || count != 2 {
					t.Fatalf("LR capture lost after stream commits: count=%d, %v", count, err)
				}
			})
		}
	}
}

// Measure live parser storage after a large first row followed by small rows.
// Input and compiled-program storage precede the baseline GC. The callback
// samples after earlier commits have crossed the pruning interval; it retains
// only the current small row. Forced GC makes this a retention measurement,
// not a throughput benchmark.
func BenchmarkStreamStorageRetainedHeap(b *testing.B) {
	for _, liveRoot := range []bool{false, true} {
		src := streamStorageGrammar
		if !liveRoot {
			src = strings.ReplaceAll(src, `h:"#head\n" #atomic`, `"#head\n"`)
			src = strings.ReplaceAll(src, ` {text($h) == "#head\n"}`, "")
		}
		g, err := parseTest(src)
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, compile.Options{})
		if err != nil {
			b.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			rule, err := prog.rule(backend, "main")
			if err != nil {
				b.Fatal(err)
			}
			for _, unit := range []PositionUnit{CodePoints, Bytes} {
				text := "#head\n" + "x" + strings.Repeat("abcdefghij,", 5000) + "\n" + strings.Repeat("xa,\n", 300)
				b.Run(fmt.Sprintf("root=%t/%s/%s", liveRoot, backend, unit), func(b *testing.B) {
					var total int64
					for b.Loop() {
						runtime.GC()
						var before, during runtime.MemStats
						runtime.ReadMemStats(&before)
						p := core.NewStreamParser(&prog.sharedProgram, strings.NewReader(text), unit)
						p.Descs = prog.descs(backend)
						count := 0
						p.Emit = func(n *value.Node) error {
							count++
							if count == 200 {
								runtime.GC()
								runtime.ReadMemStats(&during)
								total += int64(during.HeapAlloc) - int64(before.HeapAlloc)
							}
							runtime.KeepAlive(n)
							return nil
						}
						if _, ok := p.Call(rule, 0); !ok || !p.AtEOF() || count != 301 {
							b.Fatalf("count=%d pos=%d: %v", count, p.Pos, p.SyntaxError())
						}
						runtime.KeepAlive(p)
						runtime.KeepAlive(text)
					}
					b.ReportMetric(float64(total)/float64(b.N), "retained-B")
				})
			}
		}
	}
}

// Compare a large-first stream with uniform small and uniformly wide rows.
// This reports cumulative allocation and throughput; it does not force GC.
func BenchmarkStreamRetentionWorkload(b *testing.B) {
	g, err := parseTest(streamStorageGrammar)
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
			for _, pattern := range []string{"small", "mixed", "wide"} {
				row := "x" + strings.Repeat("abcdefghij,", 5000) + "\n"
				body, count := strings.Repeat("xa,\n", 3000), 3000
				if pattern == "mixed" {
					body = row + body
					count++
				}
				if pattern == "wide" {
					body = strings.Repeat(row, 20)
					count = 20
				}
				text := "#head\n" + body
				b.Run(fmt.Sprintf("%s/%s/%s", pattern, backend, unit), func(b *testing.B) {
					b.SetBytes(int64(len(text)))
					b.ReportAllocs()
					for b.Loop() {
						got := 0
						err := prog.ParseStreamWith("main", strings.NewReader(text), func(*value.Node) error { got++; return nil }, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || got != count {
							b.Fatalf("emitted %d of %d: %v", got, count, err)
						}
					}
				})
			}
		}
	}
}

// A large retained prefix must not turn normal element allocation into
// per-node allocation, and nil callbacks keep whole-repetition behavior.
func BenchmarkStreamPrefixSize(b *testing.B) {
	g, err := parseTest(strings.ReplaceAll(strings.ReplaceAll(streamStorageGrammar, `h:"#head\n" #atomic`, `h:row`), ` {text($h) == "#head\n"}`, ``))
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, compile.Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, size := range []int{32, 5000} {
		text := "x" + strings.Repeat("abcdefghij,", size) + "\n" + strings.Repeat("xa,\n", 3000)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			if _, err := prog.rule(backend, "main"); err != nil {
				b.Fatal(err)
			}
			for _, noCallback := range []bool{false, true} {
				b.Run(fmt.Sprintf("%d/%s/nil=%t", size, backend, noCallback), func(b *testing.B) {
					b.SetBytes(int64(len(text)))
					b.ReportAllocs()
					for b.Loop() {
						count := 0
						var emit func(*value.Node) error
						if !noCallback {
							emit = func(*value.Node) error { count++; return nil }
						}
						// This fixture retains the wide prefix rather than the simple header.
						// The input suffix predicate is disabled in this benchmark's grammar.
						err := prog.ParseStreamWith("main", strings.NewReader(text), emit, ParseOptions{Backend: backend})
						if err != nil || !noCallback && count != 3000 {
							b.Fatalf("count=%d: %v", count, err)
						}
					}
				})
			}
		}
	}
}
