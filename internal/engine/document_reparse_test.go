package engine

import (
	"fmt"
	"strings"
	"testing"
)

const reparseGrammar = `def main = line* $$
def line = @(?a-z)+ "\n"`

func TestDocumentNoEditPreservesRuns(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, noEdits := range []int{0, 1, 3} {
				t.Run(fmt.Sprintf("%s/%s/no-edits=%d", backend, unit, noEdits), func(t *testing.T) {
					prog := compile(t, reparseGrammar)
					opts := ParseOptions{Backend: backend, Unit: unit}
					doc, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 1000), opts)
					if err != nil {
						t.Fatal(err)
					}
					check := func() {
						t.Helper()
						if got, want := resultJSON(doc.Parse()), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
							t.Fatalf("got %s; fresh %s", got, want)
						}
					}
					check()
					for round := 0; round < 3; round++ {
						for i := 0; i < noEdits; i++ {
							check()
							if doc.resumed != 0 || doc.Stats().Evaluated != 0 || doc.Stats().Reused != 1 {
								t.Fatalf("no-edit parse did not use only the root memo: %+v, resumed %d", doc.Stats(), doc.resumed)
							}
						}
						if err := doc.Edit(1500, 1501, string(rune('x'+round))); err != nil {
							t.Fatal(err)
						}
						check()
						if doc.resumed != 999 {
							t.Fatalf("round %d resumed %d; want 999", round, doc.resumed)
						}
					}
				})
			}
		}
	}
}

func TestDocumentNoEditRunInvalidation(t *testing.T) {
	defer func(n int) { maxEdits = n }(maxEdits)
	maxEdits = 3
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, src := range []string{
				reparseGrammar,
				`def main = line* "!" $$
def line = @(?a-z)+ "\n"`, // Ordinary failure can still record the repetition.
				`def main = line* $$
def line = @(?a-z)+ "\n" #recover(skip="\n")`,
			} {
				t.Run(fmt.Sprintf("%s/%s/%s", backend, unit, src), func(t *testing.T) {
					prog := compile(t, src)
					opts := ParseOptions{Backend: backend, Unit: unit}
					doc, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 100), opts)
					if err != nil {
						t.Fatal(err)
					}
					check := func() {
						t.Helper()
						if got, want := resultJSON(doc.Parse()), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
							t.Fatalf("got %s; fresh %s", got, want)
						}
					}
					check()
					check()
					// Two edits without a parse must not promote an older record.
					for _, pos := range []int{30, 150} {
						if err := doc.Edit(pos, pos+1, "x"); err != nil {
							t.Fatal(err)
						}
					}
					check()
					if doc.resumed != 0 {
						t.Fatalf("resumed stale run across two edits: %d", doc.resumed)
					}
					check()
					if err := doc.Edit(150, 151, "y"); err != nil {
						t.Fatal(err)
					}
					check()
					if doc.resumed != 99 {
						t.Fatalf("resumed %d; want 99", doc.resumed)
					}
					check()
					// The next edit resets the generation and all reusable state.
					if err := doc.Edit(150, 151, "1"); err != nil {
						t.Fatal(err)
					}
					if len(doc.runs) != 0 || len(doc.edits) != 0 {
						t.Fatal("rollover retained old runs")
					}
					check()
					if doc.resumed != 0 {
						t.Fatal("rollover resumed old runs")
					}
					check()
					if err := doc.Edit(150, 151, "z"); err != nil {
						t.Fatal(err)
					}
					check()
					check()
					if err := doc.Edit(150, 151, "x"); err != nil {
						t.Fatal(err)
					}
					check()
					if doc.resumed != 99 {
						t.Fatalf("after recovery resumed %d; want 99", doc.resumed)
					}
				})
			}
		}
	}
}

// BenchmarkDocumentReparse measures Edit plus Parse. The optional unchanged
// Parse runs outside the timer; it must not destroy later repetition reuse.
func BenchmarkDocumentReparse(b *testing.B) {
	prog, err := Compile(mustParse(reparseGrammar), Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			for _, redundant := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/%s/redundant=%t", backend, unit, redundant), func(b *testing.B) {
					doc, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 100000), ParseOptions{Backend: backend, Unit: unit})
					if err != nil {
						b.Fatal(err)
					}
					if _, err := doc.Parse(); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if redundant {
							b.StopTimer()
							if _, err := doc.Parse(); err != nil {
								b.Fatal(err)
							}
							b.StartTimer()
						}
						if err := doc.Edit(150000, 150001, string(rune('x'+i%2))); err != nil {
							b.Fatal(err)
						}
						if _, err := doc.Parse(); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(doc.resumed), "resumed/op")
				})
			}
		}
	}
}

func TestDocumentNoEditNestedRuns(t *testing.T) {
	prog := compile(t, `def main = block* $$
def block = "{\n" line* "}\n"
def line = @(?a-z)+ "\n"`)
	text := strings.Repeat("{\n"+strings.Repeat("ab\n", 100)+"}\n", 100)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				doc, err := prog.NewDocumentWith("main", text, opts)
				if err != nil {
					t.Fatal(err)
				}
				check := func() {
					t.Helper()
					if got, want := resultJSON(doc.Parse()), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
						t.Fatalf("got %s; fresh %s", got, want)
					}
				}
				check()
				at := 50*304 + 2 + 50*3
				width := 1
				for _, replacement := range []string{"xyz", "a"} {
					check()
					if err := doc.Edit(at, at+width, replacement); err != nil {
						t.Fatal(err)
					}
					width = len(replacement)
					check()
					if doc.resumed != 198 {
						t.Fatalf("resumed %d; want 99 blocks + 99 lines", doc.resumed)
					}
				}
			})
		}
	}
}

func TestDocumentNoEditTracePanicClearsRuns(t *testing.T) {
	prog := compile(t, reparseGrammar)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				interrupt := false
				opts := ParseOptions{Backend: backend, Unit: unit, Trace: func(e TraceEvent) {
					if interrupt && e.Kind == TraceExit && e.Rule == "main" && e.Memo {
						panic("interrupt cached parse")
					}
				}}
				doc, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 100), opts)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := doc.Parse(); err != nil {
					t.Fatal(err)
				}
				if len(doc.runs) == 0 {
					t.Fatal("no initial repetition record")
				}
				if _, err := doc.Parse(); err != nil {
					t.Fatal(err)
				}
				interrupt = true
				func() {
					defer func() {
						if got := recover(); got != "interrupt cached parse" {
							t.Fatalf("panic = %v", got)
						}
					}()
					doc.Parse()
				}()
				if doc.memo.len() != 0 || len(doc.runs) != 0 || len(doc.kids) != 0 || doc.resumed != 0 {
					t.Fatal("interrupted no-edit parse retained state")
				}
				interrupt = false
				if got, want := resultJSON(doc.Parse()), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
					t.Fatalf("retry got %s; fresh %s", got, want)
				}
				if doc.resumed != 0 {
					t.Fatal("retry resumed discarded records")
				}
				if err := doc.Edit(150, 151, "x"); err != nil {
					t.Fatal(err)
				}
				if got, want := resultJSON(doc.Parse()), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
					t.Fatalf("edit got %s; fresh %s", got, want)
				}
				if doc.resumed != 99 {
					t.Fatalf("retry edit resumed %d; want 99", doc.resumed)
				}
			})
		}
	}
}
