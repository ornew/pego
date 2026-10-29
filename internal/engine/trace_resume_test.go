package engine

import (
	"fmt"
	"strings"
	"testing"
)

const traceResumeGrammar = `rule main = lines $$
rule lines = line*
rule line = [a-zé]+ #atomic "\n"`

// TestTraceDocumentReusedElements observes reuse without turning the rule calls
// skipped by repetition resumption into synthetic calls or memo hits.
func TestTraceDocumentReusedElements(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			for _, failedRoot := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/failed-root=%t", backend, unit, failedRoot), func(t *testing.T) {
					src := traceResumeGrammar
					if failedRoot {
						src = strings.Replace(src, "lines $$", `lines "!" $$`, 1)
					}
					prog := mustCompile(t, src)
					opts := ParseOptions{Backend: backend, Unit: unit}
					text := strings.Repeat("ab\n", 32)
					plain, err := prog.NewDocumentWith("main", text, opts)
					if err != nil {
						t.Fatal(err)
					}
					var pr Profile
					var events []TraceEvent
					tracedOpts := opts
					tracedOpts.Trace = func(e TraceEvent) {
						pr.Trace(e)
						events = append(events, e)
					}
					doc, err := prog.NewDocumentWith("main", text, tracedOpts)
					if err != nil {
						t.Fatal(err)
					}
					check := func(phase string, resumed int, rootMemo bool) {
						t.Helper()
						pr, events = Profile{}, nil
						n, err := doc.Parse()
						if (err != nil) != failedRoot {
							t.Fatalf("%s: error=%v; failed root=%t", phase, err, failedRoot)
						}
						got := resultJSON(n, err)
						if want := resultJSON(plain.Parse()); got != want {
							t.Fatalf("%s: traced=%s; plain=%s", phase, got, want)
						}
						if want := resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
							t.Fatalf("%s: traced=%s; fresh=%s", phase, got, want)
						}
						if doc.Stats() != plain.Stats() || doc.resumed != plain.resumed {
							t.Fatalf("%s: tracing changed work: %+v/%d versus %+v/%d", phase,
								doc.Stats(), doc.resumed, plain.Stats(), plain.resumed)
						}
						if pr.ReusedElements != resumed || doc.resumed != resumed {
							t.Fatalf("%s: profile resumed=%d, document=%d; want %d", phase, pr.ReusedElements, doc.resumed, resumed)
						}
						calls, evals, memo := 0, 0, 0
						var root TraceEvent
						for _, e := range events {
							if e.Kind == TraceEnter {
								if e.ReusedElements != 0 {
									t.Fatalf("%s: enter reports reuse: %+v", phase, e)
								}
								continue
							}
							calls++
							evals += e.Evals
							if e.Memo {
								memo++
								if e.ReusedElements != 0 {
									t.Fatalf("%s: memo hit replayed old resumption: %+v", phase, e)
								}
							}
							if e.Depth == 1 {
								root = e
							}
							if e.Rule == "lines" && e.ReusedElements != resumed {
								t.Fatalf("%s: enclosing lines call reports %d; want %d", phase, e.ReusedElements, resumed)
							}
							if e.Rule == "line" && e.ReusedElements != 0 {
								t.Fatalf("%s: fabricated resumption in an executed line call", phase)
							}
						}
						if root.Rule != "main" || root.Memo != rootMemo || root.ReusedElements != resumed {
							t.Fatalf("%s: root=%+v", phase, root)
						}
						if pr.Parses != 1 || pr.Calls != calls || pr.Evals != evals || pr.MemoHits != memo ||
							evals != doc.Stats().Evaluated || memo+resumed != doc.Stats().Reused {
							t.Fatalf("%s: profile=%+v; event counts=%d/%d/%d; document=%+v", phase, pr, calls, evals, memo, doc.Stats())
						}
						rowResumed := 0
						for _, r := range pr.Rules {
							rowResumed += r.ReusedElements
							want := 0
							if r.Rule == "main" || r.Rule == "lines" {
								want = resumed
							}
							if r.ReusedElements != want {
								t.Fatalf("%s: profile rule=%+v; want resumed=%d", phase, r, want)
							}
						}
						if resumed > 0 && rowResumed != 2*pr.ReusedElements {
							t.Fatalf("%s: nested inclusive rows=%d; root-only total=%d", phase, rowResumed, pr.ReusedElements)
						}
					}
					check("initial", 0, false)
					for _, replacement := range []string{"x", "long", "é"} {
						// The unchanged first sixteen lines precede the edit. Replacing
						// one character of line 17 changes just that element; the other
						// fifteen lines are a resumed tail, shifted when length changes.
						for _, d := range []*Document{doc, plain} {
							if err := d.Edit(48, 49, replacement); err != nil {
								t.Fatal(err)
							}
						}
						check("edit "+replacement, 31, false)
						check("no edit", 0, true)
					}
				})
			}
		}
	}
}

func TestTraceDocumentReusedElementsPanicRetry(t *testing.T) {
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []PositionUnit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				prog := mustCompile(t, traceResumeGrammar)
				opts := ParseOptions{Backend: backend, Unit: unit}
				doc, err := prog.NewDocumentWith("main", strings.Repeat("ab\n", 32), opts)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := doc.Parse(); err != nil {
					t.Fatal(err)
				}
				if err := doc.Edit(48, 49, "long"); err != nil {
					t.Fatal(err)
				}
				marker := &struct{ name string }{"trace abort"}
				var pr Profile
				tracedOpts := opts
				tracedOpts.Trace = func(e TraceEvent) {
					pr.Trace(e)
					if e.Kind == TraceExit && e.Rule == "lines" && e.ReusedElements > 0 {
						panic(marker)
					}
				}
				func() {
					defer func() {
						if got := recover(); got != marker {
							t.Fatalf("panic=%v; want original marker", got)
						}
					}()
					doc.ParseWith(tracedOpts)
				}()
				// The completed child row can report reuse, but no root exit was
				// delivered. Aggregate metrics must not count a synthetic exit.
				if pr.ReusedElements != 0 {
					t.Fatalf("aborted root contributed reuse: %d", pr.ReusedElements)
				}
				childResumed := 0
				for _, r := range pr.Rules {
					if r.Rule == "lines" {
						childResumed = r.ReusedElements
					}
				}
				if childResumed != 31 || doc.memo.Len() != 0 || len(doc.runs) != 0 {
					t.Fatalf("child resumed=%d; aborted document retained reusable state", childResumed)
				}
				pr = Profile{}
				tracedOpts.Trace = pr.Trace
				if got, want := resultJSON(doc.ParseWith(tracedOpts)), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
					t.Fatalf("retry=%s; fresh=%s", got, want)
				}
				if pr.ReusedElements != 0 || doc.resumed != 0 || pr.MemoHits+pr.ReusedElements != doc.Stats().Reused {
					t.Fatalf("retry replayed aborted reuse: profile=%+v; stats=%+v", pr, doc.Stats())
				}
			})
		}
	}
}
