package engine

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

const streamStorageGrammar = `def main = h:@"#head\n" row* #stream $$ [text($h) == "#head\n"]
def row = k:@"x" vs:value+ "\n"
def value = v:@(?a-z)+ ","`

// Completed calls can leave references beyond slice length. Stream commits
// must release them at the next prune without disturbing active root values.
func TestStreamStorageCommitReferences(t *testing.T) {
	p := newParser(compile(t, `def main = "a"`), strings.Repeat("a", 4096), CodePoints, false)
	live := &Node{Text: "live"}
	dead := &Node{Text: "dead"}
	root := p.newFrame(1)
	root.vals[0] = live
	p.frame = root
	p.trail = make([]undo, 3)
	p.trail[0] = undo{f: root, old: live}
	p.trail[2] = undo{f: &frame{vals: []*Node{dead}}, old: dead}
	p.trail = p.trail[:1]
	p.vals = []any{live, dead, dead}
	p.vals = p.vals[:1]
	p.commit(1023)
	if len(p.trail) != 0 || p.pruned != 0 || p.vals[0] != live || root.vals[0] != live {
		t.Fatal("early commit damaged live state or pruned too soon")
	}
	p.commit(1024)
	if p.pruned != 1024 || p.vals[0] != live || root.vals[0] != live {
		t.Fatal("pruning damaged live root or active VM value")
	}
	if streamUndoCleanup {
		for _, u := range p.trail[:cap(p.trail)] {
			if u.f != nil || u.old != nil {
				t.Fatal("inactive undo entry retained a completed value")
			}
		}
	}
	if streamStackCleanup {
		for _, v := range p.vals[len(p.vals):cap(p.vals)] {
			if v != nil {
				t.Fatal("inactive VM stack entry retained a completed value")
			}
		}
	}
}

func TestStreamPrefixStorageIsolation(t *testing.T) {
	if !streamPrefixIsolation {
		t.Skip("reference build shares prefix storage")
	}
	p := newStreamParser(compile(t, `def main = "a"`), strings.NewReader("a"), CodePoints)
	root := p.newFrame(1)
	p.frame = root
	prefix := p.newNode(Node{Text: "prefix"})
	root.vals[0] = prefix
	if p.nodeSlab != nil || p.ptrSlab != nil || p.frameSlab != nil {
		t.Fatal("prefix wasted or shared a bulk allocation")
	}
	p.startStreamChunks()
	next := p.newNode(Node{Text: "element"})
	element := p.newFrame(1)
	if next == prefix || &element.vals[0] == &root.vals[0] || element == root {
		t.Fatal("element allocation still shares live prefix chunks")
	}
	if p.frame != root || root.vals[0] != prefix || prefix.Text != "prefix" {
		t.Fatal("prefix isolation changed live root capture")
	}
}

// Large prefixes must switch to slabs instead of allocating every object.
func TestStreamPrefixAllocationBudget(t *testing.T) {
	if !streamPrefixIsolation {
		t.Skip("reference build uses slabs")
	}
	p := newStreamParser(compile(t, `def main = "a"`), strings.NewReader("a"), CodePoints)
	for range nodeChunk/8 + 4 {
		p.newNode(Node{Text: "prefix"})
	}
	if len(p.nodeSlab) == 0 || p.nodeChunks != 1 {
		t.Fatal("large prefix did not switch to bounded slab allocation")
	}
	p.startStreamChunks()
	if p.nodeChunks != 0 || len(p.nodeSlab) != 0 {
		t.Fatal("stream boundary did not seal large prefix storage")
	}
	p.newNode(Node{Text: "element"})
	if p.nodeChunks != 1 || len(p.nodeSlab) != nodeChunk-1 {
		t.Fatal("elements did not resume normal slab allocation")
	}
}

// Prefix captures and emitted values must survive isolation for both compact
// and wide repetition instructions, including bytecode loaded without an AST.
func TestStreamPrefixLoadedRepetitions(t *testing.T) {
	bounds := []string{"{0}", "{2}", "*"}
	if strconv.IntSize == 64 {
		bounds = append(bounds, "{0,4294967296}")
	}
	for _, bound := range bounds {
		text, count := "#head\nxx", 2
		if bound == "{0}" {
			text, count = "#head\n", 0
		}
		source := `def main = h:@"#head\n" (x:@"x")` + bound + ` #stream $$ [text($h) == "#head\n"]`
		prog := compile(t, source)
		for _, omitAST := range []bool{false, true} {
			data, err := prog.MarshalBinaryWith("main", MarshalOptions{OmitAST: omitAST})
			if err != nil {
				t.Fatal(err)
			}
			saved, _, err := LoadProgram(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				if omitAST && backend == Closure {
					continue
				}
				for _, unit := range []Unit{CodePoints, Bytes} {
					t.Run(fmt.Sprintf("%s/omitAST=%t/%s/%s", bound, omitAST, backend, unit), func(t *testing.T) {
						var retained []*Node
						err := saved.ParseStreamWith("main", strings.NewReader(text), func(n *Node) error { retained = append(retained, n); return nil }, ParseOptions{Backend: backend, Unit: unit})
						if err != nil || len(retained) != count {
							t.Fatalf("got %d rows: %v", len(retained), err)
						}
						for _, n := range retained {
							if n.Field("x").(*Node).Text != "x" {
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
	prog := compile(t, `def main = &header h:header (x:@"x")* #stream $$ [text($h) == "#head\n"]
def header = @"#head\n"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				input := "#head\n" + strings.Repeat("x", 1300)
				count := 0
				err := prog.ParseStreamWith("main", strings.NewReader(input), func(n *Node) error {
					if n.Field("x").(*Node).Text != "x" {
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
	prog := compile(t, `def main = h:(main "a" / @"a") "x"* #stream [text($h) != ""]`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				count := 0
				err := prog.ParseStreamWith("main", strings.NewReader("aa"), func(*Node) error { count++; return nil }, ParseOptions{Backend: backend, Unit: unit})
				if err != nil || count != 0 {
					t.Fatalf("LR root capture lost: count=%d, %v", count, err)
				}
			})
		}
	}
}

func TestVMStreamRepeatShape(t *testing.T) {
	prog := compile(t, `def main = (x:@"x")* #stream $$`)
	code := prog.Module().Code
	ip := -1
	for i, in := range code {
		if in.Op == OpRepeat {
			ip = i
			break
		}
	}
	if ip < 0 {
		t.Fatal("missing repetition")
	}
	vm := &vmProgram{m: &Module{Code: code}}
	if !vm.isStreamRepeat(ip) {
		t.Fatal("stream repetition not recognized")
	}
	for _, target := range []int32{-1, 0, int32(ip + 2), int32(len(code)), int32(len(code) + 1)} {
		copied := append([]Instr(nil), code...)
		copied[ip+1].A = target
		vm.m = &Module{Code: copied}
		if vm.isStreamRepeat(ip) {
			t.Fatalf("invalid target %d recognized as stream", target)
		}
	}
	for _, defect := range []string{"iterator", "end", "next", "flag"} {
		copied := append([]Instr(nil), code...)
		end := int(copied[ip+1].A)
		switch defect {
		case "iterator":
			copied[ip+1].Op = OpNext
		case "end":
			copied[end].Op = OpNext
		case "next":
			copied[end-1].Op = OpIter
		case "flag":
			copied[end-1].B = 1
		}
		vm.m = &Module{Code: copied}
		if vm.isStreamRepeat(ip) {
			t.Fatalf("invalid %s recognized as stream", defect)
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
			src = strings.ReplaceAll(src, `h:@"#head\n"`, `"#head\n"`)
			src = strings.ReplaceAll(src, ` [text($h) == "#head\n"]`, "")
		}
		g, err := syntax.Parse(src)
		if err != nil {
			b.Fatal(err)
		}
		prog, err := Compile(g, Options{})
		if err != nil {
			b.Fatal(err)
		}
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			rule, err := prog.rule(backend, "main")
			if err != nil {
				b.Fatal(err)
			}
			for _, unit := range []Unit{CodePoints, Bytes} {
				text := "#head\n" + "x" + strings.Repeat("abcdefghij,", 5000) + "\n" + strings.Repeat("xa,\n", 300)
				b.Run(fmt.Sprintf("root=%t/%s/%s", liveRoot, backend, unit), func(b *testing.B) {
					var total int64
					for b.Loop() {
						runtime.GC()
						var before, during runtime.MemStats
						runtime.ReadMemStats(&before)
						p := newStreamParser(prog, strings.NewReader(text), unit)
						p.descs = prog.descs(backend)
						count := 0
						p.emit = func(n *Node) error {
							count++
							if count == 200 {
								runtime.GC()
								runtime.ReadMemStats(&during)
								total += int64(during.HeapAlloc) - int64(before.HeapAlloc)
							}
							runtime.KeepAlive(n)
							return nil
						}
						if _, ok := p.call(rule, 0); !ok || !p.atEOF() || count != 301 {
							b.Fatalf("count=%d pos=%d: %v", count, p.pos, p.syntaxError())
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
	g, err := syntax.Parse(streamStorageGrammar)
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
						err := prog.ParseStreamWith("main", strings.NewReader(text), func(*Node) error { got++; return nil }, ParseOptions{Backend: backend, Unit: unit})
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
	g, err := syntax.Parse(strings.ReplaceAll(strings.ReplaceAll(streamStorageGrammar, `h:@"#head\n"`, `h:row`), ` [text($h) == "#head\n"]`, ``))
	if err != nil {
		b.Fatal(err)
	}
	prog, err := Compile(g, Options{})
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
						var emit func(*Node) error
						if !noCallback {
							emit = func(*Node) error { count++; return nil }
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
