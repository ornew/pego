package engine

import (
	"fmt"
	"strings"
	"testing"
)

const leftRecursionDeadTail = `def r1 = r2?
def r2 = (("a" / r1 / "\n") / ("" / "\n") / (r2 / $$)){0,1}`

var reachabilityGeneratedCases = []genCase{
	{"unreachable LR tail", leftRecursionDeadTail + "\ndef main = r2", []string{"", "a", "\n", "aa", "b"}},
	{"unreachable LR alias tail", `def r1 = r2?
def r2 = (("a" / r1 / "\n") / empty / (r2 / $$)){0,1}
def empty = alias
def alias = ""
def main = r2`, []string{"", "a", "\n", "b"}},
	{"LR cut inside optional", `def r1 = r2?
def r2 = (("a" / r1 / "\n") / ("x" -- "y")? / (r2 / $$)){0,1}
def main = r2`, []string{"", "x", "xy", "\n"}},
	{"left recursion in recovery skip", `def main = a
def a = "x" #recover(skip=a) / "y"`, []string{"", "y", "x", "b"}},
	{"unreachable recovery chain", `def r1 = r2?
def r2 = (r1 / main / r2){0,1}
def main = empty #recover(skip=main)
def empty = ""`, []string{"", "a", "\n"}},
	{"cut in recovery skip", `def main = (_|_ #recover(skip=(-- "x")))? / ""`, []string{"", "x", "b"}},
}

func TestLeftRecursionRetainsReachableSuffixes(t *testing.T) {
	for _, prior := range []string{
		`("x" -- "y")?`, `("x" -- "y")*`, `("x" -- "y" / _)`,
		`(_|_ #recover(skip=(-- "x")))?`, `(_|_ #recover(skip=(-- "x")))*`,
		`(_|_ #recover(skip=(-- "x")) / _)`, `!(_|_ #recover(skip="x"))`,
		`[false]`, `$$`, `r1`, `pratt_value`, `pratt_value(lv)`,
	} {
		t.Run(prior, func(t *testing.T) {
			p := compile(t, fmt.Sprintf("def r1 = r2?\ndef r2 = ((\"a\" / r1 / \"\\n\") / (%s) / (r2 / $$)){0,1}\ndef pratt_value = pratt { operand _ level lv { postfix \"!\" } }", prior))
			for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
				n, err := p.ParseWith("r2", "", ParseOptions{Backend: backend})
				if err != nil || n.String() != `[nil]@r2` {
					t.Errorf("%s: reachable tail classification changed: %v, %v", backend, n, err)
				}
			}
		})
	}
}

func TestLeftRecursionIgnoresUnreachableRecoveryChain(t *testing.T) {
	for _, depth := range []int{1, 4, 8} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			prefix := "def r1 = r2?\ndef r2 = (r1 / chain0 / r2){0,1}\n"
			var original, removed strings.Builder
			original.WriteString(prefix)
			removed.WriteString(prefix)
			for i := range depth {
				fmt.Fprintf(&original, "def chain%d = chain%d #recover(skip=chain%d)\n", i, i+1, i)
				fmt.Fprintf(&removed, "def chain%d = chain%d\n", i, i+1)
			}
			fmt.Fprintf(&original, "def chain%d = \"\"", depth)
			fmt.Fprintf(&removed, "def chain%d = \"\"", depth)
			for _, options := range []Options{{}, {DisableMemo: true}} {
				p, q := compile(t, original.String(), options), compile(t, removed.String(), options)
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []Unit{CodePoints, Bytes} {
						o := ParseOptions{Backend: backend, Unit: unit}
						got, err := p.ParseWith("r2", "", o)
						want, wantErr := q.ParseWith("r2", "", o)
						if resultJSON(got, err) != resultJSON(want, wantErr) {
							t.Errorf("%s/%s memo disabled=%v: dead recovery chain changes result: %v, %v vs %v, %v", backend, unit, options.DisableMemo, got, err, want, wantErr)
						}
					}
				}
			}
		})
	}
}

func TestLeftRecursionReachabilitySavedAndDocuments(t *testing.T) {
	for _, c := range reachabilityGeneratedCases {
		t.Run(c.name, func(t *testing.T) {
			for _, options := range []Options{{}, {DisableMemo: true}} {
				p := compile(t, c.src, options)
				for _, marshal := range []MarshalOptions{{}, {OmitAST: true}} {
					data, err := p.MarshalBinaryWith("main", marshal)
					if err != nil {
						t.Fatal(err)
					}
					q, _, err := LoadProgram(data, Options{})
					if err != nil {
						t.Fatal(err)
					}
					for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
						if marshal.OmitAST && backend == Closure {
							continue
						}
						for _, unit := range []Unit{CodePoints, Bytes} {
							o := ParseOptions{Backend: backend, Unit: unit}
							for _, text := range c.inputs {
								want, wantErr := p.ParseWith("main", text, o)
								got, gotErr := q.ParseWith("main", text, o)
								if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
									t.Fatalf("saved %s/%s input %q differs", backend, unit, text)
								}
								doc, err := q.NewDocumentWith("main", text, o)
								if err != nil {
									t.Fatal(err)
								}
								doc.Parse()
								for _, inserted := range []string{"y", "a", "\n", ""} {
									if err := doc.Edit(0, len(doc.Text()), inserted); err != nil {
										t.Fatal(err)
									}
									want, wantErr := q.ParseWith("main", doc.Text(), o)
									got, gotErr := doc.Parse()
									if resultJSON(got, gotErr) != resultJSON(want, wantErr) {
										t.Fatalf("Document %s/%s input %q differs from fresh", backend, unit, doc.Text())
									}
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestLeftRecursionRecoverySkipStreams(t *testing.T) {
	for _, options := range []Options{{}, {DisableMemo: true}} {
		p := compile(t, `def main = (a "\n")* #stream $$
def a = "x" #recover(skip=a) / "y"`, options)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				count := 0
				err := p.ParseStreamWith("main", strings.NewReader("y\ny\n"), func(*Node) error {
					count++
					return nil
				}, ParseOptions{Backend: backend, Unit: unit, MaxDepth: 20})
				if err != nil || count != 2 {
					t.Errorf("%s/%s memo disabled=%v: %d elements, %v", backend, unit, options.DisableMemo, count, err)
				}
			}
		}
	}
}

func TestLeftRecursionInRecoverySkip(t *testing.T) {
	p := compile(t, `def main = a
def a = "x" #recover(skip=a) / "y"`)
	for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, unit := range []Unit{CodePoints, Bytes} {
			n, err := p.ParseWith("main", "y", ParseOptions{Backend: backend, Unit: unit, MaxDepth: 20})
			if err != nil || n.String() != `"y"@a` {
				t.Errorf("%s/%s: got %v, %v; want y without a recursion-limit failure", backend, unit, n, err)
			}
		}
	}
}

func TestLeftRecursionIgnoresUnreachableAlternatives(t *testing.T) {
	const original = leftRecursionDeadTail
	const removed = `def r1 = r2?
def r2 = (("a" / r1 / "\n") / ("" / "\n")){0,1}`
	for _, options := range []Options{{}, {DisableMemo: true}} {
		p, q := compile(t, original, options), compile(t, removed, options)
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				o := ParseOptions{Backend: backend, Unit: unit}
				for _, input := range []string{"", "a", "\n", "aa", "b"} {
					got, err := p.ParseWith("r2", input, o)
					want, wantErr := q.ParseWith("r2", input, o)
					if (err == nil) != (wantErr == nil) || got.String() != want.String() {
						t.Errorf("%s/%s memo disabled=%v input %q: original %v, %v; removed %v, %v", backend, unit, options.DisableMemo, input, got, err, want, wantErr)
					}
					if input == "" && (err != nil || got.String() != `[[""]@r2]@r2`) {
						t.Errorf("%s/%s memo disabled=%v: empty result %v, %v", backend, unit, options.DisableMemo, got, err)
					}
				}
			}
		}
	}
}

func TestLeftRecursionUnconditionalSuffixForms(t *testing.T) {
	for _, prior := range []string{
		`_`, `""`, `"x"?`, `"x"*`, `-"x"?`, `@"x"*`, `x:"x"?`,
		`&"x"?`, `!_|_`, `("x" -- "y")? / _`, `"x"? #error(message="x")`,
		`empty`, `alias`, `r2{0}`, `("x" -- "y"){0}`,
	} {
		t.Run(prior, func(t *testing.T) {
			prefix := fmt.Sprintf("def r1 = r2?\ndef r2 = ((\"a\" / r1 / \"\\n\") / (%s)", prior)
			original := prefix + " / (r2 / $$)){0,1}\ndef empty = \"\"\ndef alias = empty"
			removed := prefix + "){0,1}\ndef empty = \"\"\ndef alias = empty"
			for _, options := range []Options{{}, {DisableMemo: true}} {
				p, q := compile(t, original, options), compile(t, removed, options)
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []Unit{CodePoints, Bytes} {
						for _, input := range []string{"", "a", "x", "xy", "\n", "aa", "b"} {
							o := ParseOptions{Backend: backend, Unit: unit}
							got, err := p.ParseWith("r2", input, o)
							want, wantErr := q.ParseWith("r2", input, o)
							if (err == nil) != (wantErr == nil) || got.String() != want.String() {
								t.Fatalf("%s/%s memo disabled=%v input %q: original %v, %v; removed %v, %v", backend, unit, options.DisableMemo, input, got, err, want, wantErr)
							}
						}
					}
				}
			}
		})
	}
}
