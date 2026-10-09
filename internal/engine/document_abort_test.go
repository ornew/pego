package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestDocumentAbortedParse(t *testing.T) {
	for _, c := range []struct {
		name, src, text, fixed, errorText string
		depth                             int
	}{
		{"action", `
type N struct { X int }
def main = x "a" $$
def x: N = t:@(x "a" / "a") -> new N{X:1/(2-len($t))}`, "aa", "aaa", "division by zero", DefaultMaxDepth},
		{"depth", `
def main = x $$
def x = x "a" / nested
def nested = "(" nested ")" / "a"`, "((a))", "a", "nesting too deep", 3},
		{"Pratt depth", `def main=e $$
def e = pratt { operand "x" level { prefix "-" } }`, "---x", "x", "nesting too deep", 4},
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				t.Run(fmt.Sprintf("%s/%s/%s", c.name, backend, unit), func(t *testing.T) {
					prog := compile(t, c.src)
					opts := ParseOptions{Backend: backend, Unit: unit, MaxDepth: c.depth}
					doc, err := prog.NewDocumentWith("main", c.text, opts)
					if err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 3; i++ {
						n, err := doc.Parse()
						if err == nil || !strings.Contains(err.Error(), c.errorText) {
							t.Fatalf("parse %d: got %v, %v; want %s", i, n, err, c.errorText)
						}
						if got, want := resultJSON(n, err), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
							t.Fatalf("parse %d: got %s; fresh %s", i, got, want)
						}
						if doc.memo.len() != 0 || len(doc.runs) != 0 || len(doc.kids) != 0 {
							t.Fatal("aborted parse retained reusable state")
						}
					}
					if err := doc.Edit(0, len(c.text), c.fixed); err != nil {
						t.Fatal(err)
					}
					if got, want := resultJSON(doc.Parse()), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
						t.Fatalf("after edit: got %s; fresh %s", got, want)
					}
				})
			}
		}
	}
}

func TestDocumentTracePanicRetry(t *testing.T) {
	prog := compile(t, `def main = x $$
def x = x "a" / "a"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			t.Run(fmt.Sprintf("%s/%s", backend, unit), func(t *testing.T) {
				opts := ParseOptions{Backend: backend, Unit: unit}
				opts.Trace = func(e TraceEvent) {
					// A successful recursive memo hit uses an active leader's
					// provisional best result, after the initial failing seed.
					if e.Kind == TraceExit && e.Rule == "x" && e.Memo && e.Matched {
						panic("interrupt growth")
					}
				}
				doc, err := prog.NewDocumentWith("main", "aa", opts)
				if err != nil {
					t.Fatal(err)
				}
				func() {
					defer func() {
						if got := recover(); got != "interrupt growth" {
							t.Fatalf("panic = %v", got)
						}
					}()
					doc.Parse()
				}()
				if doc.memo.len() != 0 || len(doc.runs) != 0 || len(doc.kids) != 0 {
					t.Fatal("trace panic retained reusable state")
				}
				doc.trace, opts.Trace = nil, nil
				for i := 0; i < 2; i++ {
					if got, want := resultJSON(doc.Parse()), resultJSON(prog.ParseWith("main", doc.Text(), opts)); got != want {
						t.Fatalf("retry %d: got %s; fresh %s", i, got, want)
					}
				}
			})
		}
	}
}

func TestDocumentNormalErrorsKeepMemo(t *testing.T) {
	for _, src := range []string{
		`def main = x "b" $$
def x = "a"`,
		`def main = x* $$
def x = "b" #recover(skip="a")`,
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			doc, err := compile(t, src).NewDocumentWith("main", "a", ParseOptions{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			memo := doc.memo
			first := resultJSON(doc.Parse())
			if doc.memo != memo || doc.memo.len() == 0 {
				t.Fatal("normal syntax error discarded the memo")
			}
			if got := resultJSON(doc.Parse()); got != first || doc.stats.Reused == 0 {
				t.Fatalf("%s: retry %s; first %s; stats %+v", backend, got, first, doc.stats)
			}
		}
	}
}
