package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDocumentAbortedParse(t *testing.T) {
	for _, c := range []struct {
		name, src, text, fixed, errorText string
		depth                             int
	}{
		{"action", `type N struct { X int }
rule main = x "a" $$
rule x N = t:(x "a" / "a") #atomic => N{X: 1 / (2 - len($t))}`, "aa", "aaa", "division by zero", DefaultMaxDepth},
		{"depth", `rule main = x $$
rule x = x "a" / nested
rule nested = "(" nested ")" / "a"`, "((a))", "a", "nesting too deep", 3},
		{"OperatorsRule depth", `rule main = e $$
rule e = operators {
    operand "x"
    level { prefix "-" }
}`, "---x", "x", "nesting too deep", 4},
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
						if doc.memo.len() != 0 || len(doc.runs) != 0 || len(doc.kids) != 0 || doc.vals != nil {
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
	prog := compile(t, `rule main = x $$
rule x = x "a" / "a"`)
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
				if doc.memo.len() != 0 || len(doc.runs) != 0 || len(doc.kids) != 0 || doc.vals != nil {
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

func TestDocumentLeftRecursionPanicEdits(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"direct", `rule main = x $$
rule x = x "α" / "α"`},
		{"indirect", `rule main = x $$
rule x = y "α" / "α"
rule y = x`},
	} {
		prog := compile(t, tc.source)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				t.Run(fmt.Sprintf("%s/%s/%s", tc.name, backend, unit), func(t *testing.T) {
					opts := ParseOptions{Backend: backend, Unit: unit}
					marker := new(int)
					opts.Trace = func(e TraceEvent) {
						// Interrupt after a recursive call has reused a successful
						// provisional seed, rather than before growth starts.
						if e.Kind == TraceExit && e.Rule == "x" && e.Memo && e.Matched {
							panic(marker)
						}
					}
					doc, err := prog.NewDocumentWith("main", "αα", opts)
					if err != nil {
						t.Fatal(err)
					}
					for attempt := range 2 {
						func() {
							defer func() {
								if got := recover(); got != marker {
									t.Fatalf("attempt %d: panic = %v", attempt, got)
								}
							}()
							doc.Parse()
						}()
						if doc.memo.len() != 0 || len(doc.runs) != 0 || len(doc.kids) != 0 || doc.vals != nil {
							t.Fatal("growth panic retained reusable state")
						}
					}
					doc.trace, opts.Trace = nil, nil
					for _, edit := range []struct {
						text string
						ok   bool
					}{{"ααα", true}, {"αb", false}, {"", false}, {"α", true}, {"αα", true}} {
						end := utf8.RuneCountInString(doc.Text())
						if unit == Bytes {
							end = len(doc.Text())
						}
						if err := doc.Edit(0, end, edit.text); err != nil {
							t.Fatal(err)
						}
						for retry := range 2 {
							n, err := doc.Parse()
							if (err == nil) != edit.ok || edit.ok && n == nil {
								t.Fatalf("text %q retry %d: node=%v error=%v", edit.text, retry, n, err)
							}
							if !edit.ok {
								var syntaxError *SyntaxError
								if !errors.As(err, &syntaxError) {
									t.Fatalf("text %q retry %d: expected syntax error, got %v", edit.text, retry, err)
								}
								wantCol := 1
								if edit.text != "" {
									wantCol = 2
									if unit == Bytes {
										wantCol = 3
									}
								}
								if syntaxError.Line != 1 || syntaxError.Col != wantCol {
									t.Fatalf("text %q retry %d: error location %d:%d; want 1:%d", edit.text, retry, syntaxError.Line, syntaxError.Col, wantCol)
								}
							}
							if got, want := resultJSON(n, err), resultJSON(prog.ParseWith("main", edit.text, opts)); got != want {
								t.Fatalf("text %q retry %d: Document %s; fresh %s", edit.text, retry, got, want)
							}
						}
					}
				})
			}
		}
	}
}

func TestDocumentNormalErrorsKeepMemo(t *testing.T) {
	for _, src := range []string{
		`rule main = x "b" $$
rule x = "a"`,
		`rule main = x* $$
rule x = "b" #recover(skip="a")`,
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
