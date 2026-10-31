package document_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"

	"github.com/ornew/zeroparse/internal/engine/document"

	"github.com/ornew/zeroparse/internal/compile"
	. "github.com/ornew/zeroparse/internal/engine"
	"github.com/ornew/zeroparse/internal/engine/closure"
	"github.com/ornew/zeroparse/internal/engine/core"
	"github.com/ornew/zeroparse/internal/engine/vm"
	"github.com/ornew/zeroparse/internal/value"
)

func BenchmarkDocumentVMValuesUnchanged(b *testing.B) {
	prog, err := Compile(mustParse(reparseGrammar), compile.Options{})
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
				if _, err := doc.Parse(); err != nil || doc.Stats().Evaluated != 0 {
					b.Fatalf("cached parse evaluated a rule: %v", err)
				}
			}
		})
	}
}

// A Document keeps the backend state of its last parse, but not the parser that ran it: the
// parser holds the whole parse state (runs, trail, slabs), which the next parse replaces.
func TestDocumentKeptMachineReleasesParser(t *testing.T) {
	// The lambda makes the closure backend use its machine too.
	prog := mustCompile(t, `rule main = ls:line* $$ => map($ls, (l) => l)
rule line = [a-z]+ "\n"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		t.Run(backend.String(), func(t *testing.T) {
			doc, err := prog.NewDocumentWith("main", strings.Repeat("abc\n", 1000), ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 { // the second parse attaches the kept machine to a new parser
				var parsed weak.Pointer[core.Parser]
				if _, err := doc.ParseWithCapture(doc.ParseDefaults(), func(p *core.Parser, _ *value.Node, err error) error {
					parsed = weak.Make(p)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				switch mc := doc.Machine().(type) {
				case *vm.Machine:
					if mc.Parser() != nil {
						t.Fatal("kept machine refers to the finished parser")
					}
				case *closure.Machine:
					if mc.P != nil {
						t.Fatal("kept machine refers to the finished parser")
					}
				default:
					t.Fatalf("document kept %T", doc.Machine())
				}
				for range 5 {
					runtime.GC()
				}
				if parsed.Value() != nil {
					t.Fatal("finished parser is still reachable from the Document")
				}
			}
		})
	}
}

func TestDocumentVMValueStorage(t *testing.T) {
	prog := mustCompile(t, `rule main = line* $$
rule line = [a-z / "é" / "😀"]+ #atomic "\n"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				doc, err := prog.NewDocumentWith("main", strings.Repeat("abé😀\n", 5000), opts)
				if err != nil {
					t.Fatal(err)
				}
				check := func() *value.Node {
					t.Helper()
					n, err := doc.Parse()
					if got, want := resultJSON(n, err), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
						t.Fatal("reused document differs from fresh parsing")
					}
					if len(documentVMValues(doc)) != 0 {
						t.Fatal("completed parse retained an active VM stack")
					}
					for _, v := range documentVMValues(doc)[:cap(documentVMValues(doc))] {
						if v != nil {
							t.Fatal("VM scratch retained a result or transient value")
						}
					}
					return n
				}
				n := check()
				if len(value.D(n).Children) != 1 || value.D(n).Children[0].Type() != value.TypeList || len(value.D(value.D(n).Children[0]).Children) != 5000 {
					t.Fatal("expected one list containing all input lines")
				}
				retained := append([]*value.Node(nil), value.D(value.D(n).Children[0]).Children[:3]...)
				before := make([]string, len(retained))
				for i, child := range retained {
					before[i] = resultJSON(child, nil)
				}
				var backing *any
				if document.ReuseDocumentVMValues && backend != Closure {
					if cap(documentVMValues(doc)) < 5000 {
						t.Fatal("large parse did not keep its VM value storage")
					}
					backing = &documentVMValues(doc)[:cap(documentVMValues(doc))][0]
				} else if documentVMValues(doc) != nil {
					t.Fatal("closure or reference parse retained VM value storage")
				}
				check()
				if doc.Stats().Evaluated != 0 {
					t.Fatal("unchanged parse did not reuse the whole root")
				}
				at := doc.Input().Loaded() / 2
				if err := doc.Edit(at, at+1, "x"); err != nil {
					t.Fatal(err)
				}
				check()
				if backing != nil && &documentVMValues(doc)[:cap(documentVMValues(doc))][0] != backing {
					t.Fatal("same-sized reparse replaced the reusable VM stack")
				}
				if err := doc.Edit(doc.Input().Loaded()-1, doc.Input().Loaded(), "!"); err != nil {
					t.Fatal(err)
				}
				check() // An ordinary syntax error must still clear every slot.
				check() // Repeated failure can reuse memoized work.
				if err := doc.Edit(doc.Input().Loaded()-1, doc.Input().Loaded(), "\n"); err != nil {
					t.Fatal(err)
				}
				check()
				if backing != nil {
					dirty := documentVMValues(doc)[:cap(documentVMValues(doc))]
					doc.SetDefaultTrace(func(e TraceEvent) {
						if e.Kind == TraceExit && e.Rule == "line" && !e.Memo && e.Matched {
							panic("stop after writing values")
						}
					})
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
					if documentVMValues(doc) != nil || doc.Stats().Evaluated == 0 {
						t.Fatal("dirty-stack abort did not discard reusable state")
					}
					for _, v := range dirty {
						if v != nil {
							t.Fatal("dirty-stack interruption retained a value")
						}
					}
					doc.SetDefaultTrace(nil)
					check()
				}
				if err := doc.Edit(0, doc.Input().Loaded(), "é😀\n"); err != nil {
					t.Fatal(err)
				}
				check() // A small parse after a large one must not expose old values.
				if cap(documentVMValues(doc)) >= 5000 {
					t.Fatal("small document retained the large input's VM stack")
				}
				for i, child := range retained {
					if resultJSON(child, nil) != before[i] {
						t.Fatal("stack reuse changed a retained prefix node")
					}
				}
				if backing != nil {
					doc.SetDefaultTrace(func(e TraceEvent) {
						if e.Kind == TraceEnter {
							panic("stop before cached root")
						}
					})
					func() {
						defer func() {
							if recover() != "stop before cached root" {
								t.Fatal("expected trace interruption")
							}
						}()
						doc.Parse()
					}()
					if documentVMValues(doc) != nil {
						t.Fatal("aborted parse retained its VM stack")
					}
					doc.SetDefaultTrace(nil)
					check()
				}
			})
		}
	}
}

func benchmarkDocumentVMValuesShrink(b *testing.B, edit bool) {
	prog, err := Compile(mustParse(reparseGrammar), compile.Options{})
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
				if err := doc.Edit(0, doc.Input().Loaded(), small); err != nil {
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
				b.ReportMetric(float64(cap(documentVMValues(doc)))*float64(unsafe.Sizeof(any(nil))), "stack-bytes")
			})
		}
	}
}

// documentVMValues returns the VM value stack that the Document d keeps for its next parse, or nil.
func documentVMValues(d *Document) []any {
	if mc, ok := d.Machine().(*vm.Machine); ok {
		return mc.Values()
	}
	return nil
}

func BenchmarkDocumentVMValuesShrink(b *testing.B) {
	benchmarkDocumentVMValuesShrink(b, false)
}

func BenchmarkDocumentVMValuesShrinkEdits(b *testing.B) {
	benchmarkDocumentVMValuesShrink(b, true)
}

func TestDocumentVMValueStorageForEmptyInput(t *testing.T) {
	// Many value-building nullable calls need a large stack despite an empty
	// input. This exercises dropping new storage at return, rather than only
	// dropping an old large document's storage before a smaller parse.
	prog := mustCompile(t, "rule main = "+strings.Repeat("item ", 2048)+"$$\nrule item = \"\" #atomic")
	for _, backend := range []Backend{Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				doc, err := prog.NewDocumentWith("main", "", opts)
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					n, err := doc.Parse()
					if err != nil || n == nil || len(value.D(n).Children) != 2048 {
						t.Fatalf("empty sequence: %v, %v", n, err)
					}
					if documentVMValues(doc) != nil {
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
