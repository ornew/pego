package engine

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"
)

func TestDocumentVMValueStorage(t *testing.T) {
	prog := compile(t, `def main = line* $$
def line = @(?a-z / "é" / "😀")+ "\n"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				doc, err := prog.NewDocumentWith("main", strings.Repeat("abé😀\n", 5000), opts)
				if err != nil {
					t.Fatal(err)
				}
				check := func() *Node {
					t.Helper()
					n, err := doc.Parse()
					if got, want := resultJSON(n, err), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
						t.Fatal("reused document differs from fresh parsing")
					}
					if len(doc.vals) != 0 {
						t.Fatal("completed parse retained an active VM stack")
					}
					for _, v := range doc.vals[:cap(doc.vals)] {
						if v != nil {
							t.Fatal("VM scratch retained a result or transient value")
						}
					}
					return n
				}
				n := check()
				if len(n.Children) != 1 || n.Children[0].Type() != TypeList || len(n.Children[0].Children) != 5000 {
					t.Fatal("expected one list containing all input lines")
				}
				retained := append([]*Node(nil), n.Children[0].Children[:3]...)
				before := make([]string, len(retained))
				for i, child := range retained {
					before[i] = resultJSON(child, nil)
				}
				var backing *any
				if reuseDocumentVMValues && backend != Closure {
					if cap(doc.vals) < 5000 {
						t.Fatal("large parse did not keep its VM value storage")
					}
					backing = &doc.vals[:cap(doc.vals)][0]
				} else if doc.vals != nil {
					t.Fatal("closure or reference parse retained VM value storage")
				}
				check()
				if doc.stats.Evaluated != 0 {
					t.Fatal("unchanged parse did not reuse the whole root")
				}
				at := doc.in.loaded() / 2
				if err := doc.Edit(at, at+1, "x"); err != nil {
					t.Fatal(err)
				}
				check()
				if backing != nil && &doc.vals[:cap(doc.vals)][0] != backing {
					t.Fatal("same-sized reparse replaced the reusable VM stack")
				}
				if err := doc.Edit(doc.in.loaded()-1, doc.in.loaded(), "!"); err != nil {
					t.Fatal(err)
				}
				check() // An ordinary syntax error must still clear every slot.
				check() // Repeated failure can reuse memoized work.
				if err := doc.Edit(doc.in.loaded()-1, doc.in.loaded(), "\n"); err != nil {
					t.Fatal(err)
				}
				check()
				if backing != nil {
					dirty := doc.vals[:cap(doc.vals)]
					doc.trace = func(e TraceEvent) {
						if e.Kind == TraceExit && e.Rule == "line" && !e.Memo && e.Matched {
							panic("stop after writing values")
						}
					}
					if err := doc.Edit(at, at+1, "y"); err != nil {
						t.Fatal(err)
					}
					func() {
						defer func() {
							if recover() != "stop after writing values" {
								t.Fatal("expected dirty-stack interruption")
							}
						}()
						doc.Parse()
					}()
					if doc.vals != nil || doc.stats.Evaluated == 0 {
						t.Fatal("dirty-stack abort did not discard reusable state")
					}
					for _, v := range dirty {
						if v != nil {
							t.Fatal("dirty-stack interruption retained a value")
						}
					}
					doc.trace = nil
					check()
				}
				if err := doc.Edit(0, doc.in.loaded(), "é😀\n"); err != nil {
					t.Fatal(err)
				}
				check() // A small parse after a large one must not expose old values.
				if cap(doc.vals) >= 5000 {
					t.Fatal("small document retained the large input's VM stack")
				}
				for i, child := range retained {
					if resultJSON(child, nil) != before[i] {
						t.Fatal("stack reuse changed a retained prefix node")
					}
				}
				if backing != nil {
					doc.trace = func(e TraceEvent) {
						if e.Kind == TraceEnter {
							panic("stop before cached root")
						}
					}
					func() {
						defer func() {
							if recover() != "stop before cached root" {
								t.Fatal("expected trace interruption")
							}
						}()
						doc.Parse()
					}()
					if doc.vals != nil {
						t.Fatal("aborted parse retained its VM stack")
					}
					doc.trace = nil
					check()
				}
			})
		}
	}
}

func BenchmarkDocumentVMValuesShrink(b *testing.B) {
	benchmarkDocumentVMValuesShrink(b, false)
}

func TestDocumentVMValueStorageForEmptyInput(t *testing.T) {
	// Many value-building nullable calls need a large stack despite an empty
	// input. This exercises dropping new storage at return, rather than only
	// dropping an old large document's storage before a smaller parse.
	prog := compile(t, "def main = "+strings.Repeat("item ", 2048)+"$$\ndef item = @\"\"")
	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				doc, err := prog.NewDocumentWith("main", "", opts)
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					n, err := doc.Parse()
					if err != nil || n == nil || len(n.Children) != 2048 {
						t.Fatalf("empty sequence: %v, %v", n, err)
					}
					if doc.vals != nil {
						t.Fatal("empty input retained an oversized VM stack")
					}
					if got, want := resultJSON(n, err), resultJSON(prog.ParseWith("main", "", opts)); got != want {
						t.Fatal("discarded stack changed the nullable sequence")
					}
				}
			})
		}
	}
}

func BenchmarkDocumentVMValuesShrinkEdits(b *testing.B) {
	benchmarkDocumentVMValuesShrink(b, true)
}

func benchmarkDocumentVMValuesShrink(b *testing.B, edit bool) {
	prog, err := Compile(mustParse(reparseGrammar), Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, valid := range []bool{true, false} {
			b.Run(fmt.Sprintf("%s/valid=%t", backend, valid), func(b *testing.B) {
				doc, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 50000), ParseOptions{Backend: backend})
				if err != nil {
					b.Fatal(err)
				}
				if _, err := doc.Parse(); err != nil {
					b.Fatal(err)
				}
				small := "ab\n"
				if !valid {
					small = "!\n"
				}
				if err := doc.Edit(0, doc.in.loaded(), small); err != nil {
					b.Fatal(err)
				}
				if _, err := doc.Parse(); (err == nil) != valid {
					b.Fatalf("small input valid=%t: %v", valid, err)
				}
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					if edit {
						ins := "x"
						if i%2 != 0 {
							ins = "y"
						}
						if !valid {
							ins = "!"
							if i%2 != 0 {
								ins = "?"
							}
						}
						if err := doc.Edit(0, 1, ins); err != nil {
							b.Fatal(err)
						}
						i++
					}
					if _, err := doc.Parse(); (err == nil) != valid {
						b.Fatalf("small input valid=%t: %v", valid, err)
					}
				}
				b.ReportMetric(float64(cap(doc.vals))*float64(unsafe.Sizeof(any(nil))), "stack-bytes")
			})
		}
	}
}

func BenchmarkDocumentVMValuesUnchanged(b *testing.B) {
	prog, err := Compile(mustParse(reparseGrammar), Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		b.Run(backend.String(), func(b *testing.B) {
			doc, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 50000), ParseOptions{Backend: backend})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := doc.Parse(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := doc.Parse(); err != nil || doc.stats.Evaluated != 0 {
					b.Fatalf("cached parse evaluated a rule: %v", err)
				}
			}
		})
	}
}
