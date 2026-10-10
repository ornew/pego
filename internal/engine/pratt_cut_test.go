package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// One shared grammar keeps the same scope/selection cases in the engine and generated Go/TS corpus.
const prefixPartCuts = `
type Atom terminal
type Prefix struct { Op Match, X Expr, Tag int }
type Expr = Atom | Prefix | Error
def main: Expr = value:entry $$ -> $value
def entry: Expr = case0 / case1 / case2 / case3 / case4 / case5 / case6 / case7 / case8 / case9 / case10 / case11 / case12 / case13 / case14 / case15 / case16 / case17 / case18 / case19 / case20
def case0: Expr = "c" x:committed -> $x
def case1: Expr = "n" x:ordinary -> $x
def case2: Expr = "b" x:before -> $x
def case3: Expr = "w" x:winnerFirst -> $x
def case4: Expr = "f" x:failedFirst -> $x
def case5: Expr = "h" x:longerWinner -> $x
def case6: Expr = "s" x:shorterWinner -> $x
def case7: Expr = "t" x:ties -> $x
def case8: Expr = "k" x:winnerCut -> $x
def case9: Expr = "q" x:choiceLocal -> $x
def case10: Expr = "j" x:ruleLocal -> $x
def case11: Expr = "u" x:unicode -> $x
def case12: Expr = "v" x:trivia -> $x
def case13: Expr = "z" x:emptyCut -> $x
def case14: Expr = "o" x:emptyOrdinary -> $x
def case15: Expr = "p" x:nestedCut -> $x
def case16: Expr = "d" x:nestedOrdinary -> $x
def case17: Expr = "m" !committed x:committed -> $x
def case18: Expr = "l" !committed x:atom -> $x
def case19: Expr = "r" x:recovered -> $x
def case20: Expr = "i" x:prattPartLocal -> $x

def atom: Atom = "~-x" / "-xy" / "¬é" / "-x" / "-z" / "-" / "a" / "é"
def committed: Expr = pratt {
    operand atom
    level { prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def ordinary: Expr = pratt {
    operand atom
    level { prefix "-" "z" -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def before: Expr = pratt {
    operand atom
    level { prefix "-" "z" -- "t" -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def winnerFirst: Expr = pratt {
    operand atom
    level {
        prefix "-" -> new Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
def failedFirst: Expr = pratt {
    operand atom
    level {
        prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-" -> new Prefix{Op: $op, X: $rhs, Tag: 1}
    }
}
def longerWinner: Expr = pratt {
    operand atom
    level {
        prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-x" -> new Prefix{Op: $op, X: $rhs, Tag: 1}
    }
}
def shorterWinner: Expr = pratt {
    operand atom
    level {
        prefix "--" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-" -> new Prefix{Op: $op, X: $rhs, Tag: 1}
    }
}
def ties: Expr = pratt {
    operand atom
    level {
        prefix "-" -> new Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
        prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 3}
    }
}
def winnerCut: Expr = pratt {
    operand atom
    level {
        prefix "-" -- -> new Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
def choiceLocal: Expr = pratt {
    operand atom
    level { prefix ("-" -- "z" / "-") "q" -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def ruleLocal: Expr = pratt {
    operand atom
    level { prefix part -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def part = "-" -- "z"
def unicode: Expr = pratt {
    operand atom
    level { prefix "¬" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def trivia: Expr = pratt {
    skip " "*
    operand atom
    level { prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def emptyCut: Expr = pratt {
    operand atom
    level { prefix _ -- -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def emptyOrdinary: Expr = pratt {
    operand atom
    level { prefix _ -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
def nestedCut: Expr = pratt {
    operand atom
    level {
        prefix "~" -- -> new Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
def nestedOrdinary: Expr = pratt {
    operand atom
    level {
        prefix "~" -> new Prefix{Op: $op, X: $rhs, Tag: 1}
        prefix "-" -- "z" -> new Prefix{Op: $op, X: $rhs, Tag: 2}
    }
}
def recovered: Expr = committed #recover(skip=.*)
def prattPartLocal: Expr = pratt {
    operand atom
    level { prefix committed -> new Prefix{Op: $op, X: $rhs, Tag: 1} }
}
`

var prefixPartCutInputs = []struct {
	input string
	kind  string // Empty means unrecovered failure; Error means successful recovery.
}{
	{"c-x", ""}, {"c-", ""}, {"c-za", "Prefix"}, {"ca", "Atom"},
	{"n-x", "Atom"}, {"n-", "Atom"}, {"n-za", "Prefix"},
	{"b-x", "Atom"}, {"b-z", ""}, {"b-zta", "Prefix"},
	{"w-a", "Prefix"}, {"w-", "Atom"}, {"w-x", "Atom"},
	{"f-a", "Prefix"}, {"f-", "Atom"}, {"f-x", "Atom"},
	{"h-xa", "Prefix"}, {"h-x", "Atom"}, {"s--a", "Prefix"},
	{"t-a", "Prefix"}, {"k-a", "Prefix"}, {"k-", ""}, {"k-x", ""},
	{"q-x", "Atom"}, {"j-x", "Atom"},
	{"u¬é", ""}, {"u¬zé", "Prefix"},
	{"v -x", ""}, {"v a", "Atom"}, {"v -z a", "Prefix"},
	{"za", ""}, {"oa", "Atom"},
	{"p~-x", ""}, {"p~a", "Prefix"}, {"d~-x", "Atom"},
	{"m-x", ""}, {"l-x", "Atom"}, {"r-x", TypeError}, {"r-za", "Prefix"}, {"i-x", "Atom"}, {"iaa", "Prefix"},
}

func prefixPartCutCorpus() genCase {
	c := genCase{name: "Pratt prefix-part cuts", src: prefixPartCuts}
	for _, in := range prefixPartCutInputs {
		c.inputs = append(c.inputs, in.input)
	}
	return c
}

func TestPrattPrefixPartCuts(t *testing.T) {
	for _, disableMemo := range []bool{false, true} {
		prog := compile(t, prefixPartCuts, Options{DisableMemo: disableMemo})
		for _, c := range prefixPartCutInputs {
			t.Run(fmt.Sprintf("%s/memo=%t", c.input, !disableMemo), func(t *testing.T) {
				for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
					for _, unit := range []Unit{CodePoints, Bytes} {
						n, err := prog.ParseWith("main", c.input, ParseOptions{Backend: backend, Unit: unit})
						if c.kind == "" {
							if n != nil || err == nil || !strings.Contains(err.Error(), "syntax error") {
								t.Fatalf("%v/%v: expected syntax failure, got %v, %v", backend, unit, n, err)
							}
						} else if n == nil || n.Type() != c.kind {
							t.Fatalf("%v/%v: got %v, %v; want %s", backend, unit, n, err, c.kind)
						} else if c.kind == TypeError {
							var recovered SyntaxErrors
							if !errors.As(err, &recovered) || len(recovered) != 1 {
								t.Fatalf("%v/%v: expected one recovered error, got %v", backend, unit, err)
							}
						} else if err != nil {
							t.Fatalf("%v/%v: unexpected error: %v", backend, unit, err)
						}
						if n != nil && n.Type() == "Prefix" {
							if tag := fmt.Sprint(n.Field("Tag")); tag != "1" {
								t.Fatalf("%v/%v: selected wrong prefix: tag %s", backend, unit, tag)
							}
							wantOp := "-"
							switch c.input[:1] {
							case "c", "n", "v", "r":
								wantOp = "-z"
							case "b":
								wantOp = "-zt"
							case "h":
								wantOp = "-x"
							case "u":
								wantOp = "¬z"
							case "p":
								wantOp = "~"
							case "i":
								wantOp = "a"
							}
							op, ok := n.Field("Op").(*Node)
							if !ok || op.Text != wantOp {
								t.Fatalf("%v/%v: wrong operator capture: %v; want %q", backend, unit, op, wantOp)
							}
						}
						checkRecognize(t, prog, "main", c.input, unit, err)
					}
				}
				checkBackends(t, prog, "main", c.input)
				checkUnits(t, prog, "main", c.input, Closure)
			})
		}
	}
}

// Compilation is outside the timer. These accepted inputs perform equivalent work on the old and
// new cut contracts; committed failures have deliberately different outcomes and are tested above.
func BenchmarkPrattPrefixPartCut(b *testing.B) {
	prog, err := Compile(mustParse(prefixPartCuts), Options{})
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range []struct{ name, input string }{
		{"operand", "ca"}, {"matched-cut", "c-za"}, {"competing-uncut-fallback", "f-x"},
	} {
		for _, backend := range []Backend{Closure, Bytecode, BytecodeIterative} {
			for _, unit := range []Unit{CodePoints, Bytes} {
				b.Run(fmt.Sprintf("%s/%s/unit=%d", c.name, backend, unit), func(b *testing.B) {
					opts := ParseOptions{Backend: backend, Unit: unit}
					b.ReportAllocs()
					for b.Loop() {
						if _, err := prog.ParseWith("main", c.input, opts); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
