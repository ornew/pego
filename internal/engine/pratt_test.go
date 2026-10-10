package engine

import (
	"strings"
	"testing"
)

// arith is a grammar with a Pratt expression that builds a CST (no actions).
const arith = `
def main = e $$
def e = pratt {
    skip " "*
    operand @(?0-9)+ / "(" x:e ")"
    level { infix none "==" }
    level { infix left "+" / "-" }
    level { infix left "*" / "/" }
    level { prefix "-" }
    level { infix right "^" }
    level { postfix "!" }
}`

func TestPrattCST(t *testing.T) {
	check(t, arith,
		ok("1", `(Seq "1"@e)@main`),
		ok("1+2*3", `(Seq (Operator "1"@e "+" (Operator "2"@e "*" "3"@e operator=2)@e operator=1)@e)@main`),
		ok("1 - 2 - 3", `(Seq (Operator (Operator "1"@e "-" "2"@e operator=1)@e "-" "3"@e operator=1)@e)@main`),
		ok("2^3^4", `(Seq (Operator "2"@e "^" (Operator "3"@e "^" "4"@e operator=4)@e operator=4)@e)@main`),
		ok("-2^2", `(Seq (Operator "-" (Operator "2"@e "^" "2"@e operator=4)@e operator=3)@e)@main`),
		ok("-2*3", `(Seq (Operator (Operator "-" "2"@e operator=3)@e "*" "3"@e operator=2)@e)@main`),
		ok("2^-1", `(Seq (Operator "2"@e "^" (Operator "-" "1"@e operator=3)@e operator=4)@e)@main`),
		ok("3!!", `(Seq (Operator (Operator "3"@e "!" operator=5)@e "!" operator=5)@e)@main`),
		ok("(1+2)*3", `(Seq (Operator (Seq "(" (Operator "1"@e "+" "2"@e operator=1)@e ")" x=(Operator "1"@e "+" "2"@e operator=1)@e)@e "*" "3"@e operator=2)@e)@main`),
		ok("1==2", `(Seq (Operator "1"@e "==" "2"@e operator=0)@e)@main`),
		ok("1+1==2*1", `(Seq (Operator (Operator "1"@e "+" "1"@e operator=1)@e "==" (Operator "2"@e "*" "1"@e operator=2)@e operator=0)@e)@main`),
		// Non-associative operators do not chain.
		fails("1==2==3", `1:5: syntax error: expected "!", "*", "+", "-", "/", "^", (?0-9), end of input`),
		fails("1+", `1:3: syntax error: expected "(", "-", (?0-9)`),
		fails("", `expected "(", "-", (?0-9)`),
	)
}

func TestPrattPositions(t *testing.T) {
	prog := compile(t, arith)
	n, err := prog.Parse("e", " 1 + 2 * 3")
	if err != nil {
		t.Fatal(err)
	}
	if n.Start != 1 || n.End != 10 {
		t.Errorf("root [%d,%d)", n.Start, n.End)
	}
	mul := n.Children[2]
	if mul.Start != 5 || mul.End != 10 {
		t.Errorf("mul [%d,%d)", mul.Start, mul.End)
	}
	if op := n.Children[1]; op.Start != 3 || op.End != 4 {
		t.Errorf("op [%d,%d)", op.Start, op.End)
	}
}

const ast = `
type Num terminal
type Bin struct { L Expr, Op Match, R Expr }
type Neg struct { X Expr }
type Cond struct { C Expr, T Expr, F Expr }
type Call struct { Fn Expr, Args []Expr }
type Index struct { X Expr, I Expr }
type Expr = Num | Bin | Neg | Cond | Call | Index | Match

def main = x:e $$ -> $x
def num: Num = (?0-9)+
def ident = @(?a-z)+
def args = first:e(assignment) rest:(" "* "," r:e(assignment))*
    -> concat(list($first), map($rest, (x) => $x.r))
def e: Expr = pratt {
    skip " "*
    operand num
    operand ident
    operand "(" x:e ")" -> $x
    level { infix left "," -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level assignment { infix right "=" -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix right "?" t:e " "* ":" -> new Cond{C: $lhs, T: $t, F: $rhs} }
    level { infix right "->" -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { infix left "+" / "-" -> new Bin{L: $lhs, Op: $op, R: $rhs} }
    level { prefix "-" -> new Neg{X: $rhs} }
    level {
        postfix "(" args:args? " "* ")" -> new Call{Fn: $lhs, Args: concat($args)}
        postfix "[" i:e "]" -> new Index{X: $lhs, I: $i}
    }
}`

func TestPrattActions(t *testing.T) {
	check(t, ast,
		ok("1+2-3", `(Bin L=(Bin L=Num"1"@num Op="+" R=Num"2"@num) Op="-" R=Num"3"@num)`),
		ok("a ? b : c ? d : e", `(Cond C="a"@ident F=(Cond C="c"@ident F="e"@ident T="d"@ident) T="b"@ident)`),
		ok("a ? b = c : d", `(Cond C="a"@ident F="d"@ident T=(Bin L="b"@ident Op="=" R="c"@ident))`),
		ok("-a(1)[2]", `(Neg X=(Index I=Num"2"@num X=(Call Args=[Num"1"@num] Fn="a"@ident)))`),
		ok("f()", `(Call Args=[] Fn="f"@ident)`),
		// The comma operator is not parsed inside arguments.
		ok("f(a = 1, b), c", `(Bin L=(Call Args=[(Bin L="a"@ident Op="=" R=Num"1"@num) "b"@ident] Fn="f"@ident) Op="," R="c"@ident)`),
		// "-" and "->" are distinguished by longest match even at different binding levels.
		ok("a->b-c", `(Bin L="a"@ident Op="->" R=(Bin L="b"@ident Op="-" R="c"@ident))`),
		ok("a-b->c", `(Bin L=(Bin L="a"@ident Op="-" R="b"@ident) Op="->" R="c"@ident)`),
		ok("(a)", `"a"@ident`),
	)
}

func TestPrattPositionsWithActions(t *testing.T) {
	prog := compile(t, ast)
	n, err := prog.Parse("e", "a + b + c")
	if err != nil {
		t.Fatal(err)
	}
	if n.Start != 0 || n.End != 9 {
		t.Errorf("root [%d,%d)", n.Start, n.End)
	}
	if l := n.Field("L").(*Node); l.Start != 0 || l.End != 5 {
		t.Errorf("left [%d,%d)", l.Start, l.End)
	}
}

func TestPrattJuxtaposition(t *testing.T) {
	check(t, `
type App struct { F Expr, X Expr }
type Expr = App | Match
def main = x:e $$ -> $x
def e: Expr = pratt {
    skip " "*
    operand @(?a-z)+
    level { infix left "+" -> new App{F: $lhs, X: $rhs} }
    level { infix left _ -> new App{F: $lhs, X: $rhs} }
}`,
		ok("f x y", `(App F=(App F="f"@e X="x"@e) X="y"@e)`),
		ok("f x + g", `(App F=(App F="f"@e X="x"@e) X="g"@e)`),
	)
}

func TestPrattCut(t *testing.T) {
	src := `
def main = e "+"?
def e = pratt {
    operand @(?0-9)+
    level { infix left "+" -- }
}`
	check(t, src,
		// The operator part is a Seq, so the Operator's child is a Seq too.
		ok("1+2", `(Seq (Operator "1"@e (Seq "+") "2"@e operator=0)@e nil)@main`),
		// Without the cut, the "+" after "1" would match the outer "+"?, but the cut makes it fail.
		fails("1+", `1:3: syntax error: expected (?0-9)`),
	)
	check(t, `
def main = e "+"?
def e = pratt {
    operand @(?0-9)+
    level { infix left "+" }
}`,
		ok("1+", `(Seq "1"@e "+")@main`),
	)
}

func TestPrattPrefixFallback(t *testing.T) {
	check(t, `
def main = e
def e = pratt {
    operand "!" / @(?0-9)+
    level { prefix "!" }
}`,
		ok("!1", `(Operator "!" "1"@e operator=0)@e`),
		// If no operand follows a prefix operator, it is reparsed as an operand.
		ok("!", `"!"@e`),
	)
}

func TestPrattLevelMemo(t *testing.T) {
	// The same position yields different results at different binding levels.
	check(t, `
def main = a:e(mul) "+" b:e
def e = pratt {
    operand @(?0-9)+
    level { infix left "+" }
    level mul { infix left "*" }
}`,
		ok("1*2+3+4", `(Seq (Operator "1"@e "*" "2"@e operator=1)@e "+" (Operator "3"@e "+" "4"@e operator=0)@e a=(Operator "1"@e "*" "2"@e operator=1)@e b=(Operator "3"@e "+" "4"@e operator=0)@e)@main`),
	)
}

const restrictedPrefix = `
type Atom terminal
type Prefix struct { Op Match, X Expr }
type Binary struct { L Expr, Op Match, R Expr }
type Postfix struct { X Expr, Op Match }
type Expr = Atom | Prefix | Binary | Postfix

def main = x:e(mul) $$ -> $x
def atom: Atom = "é" / "x" / "y" / "z"
def e: Expr = pratt {
    operand atom
    level loose {
        prefix "~" -> new Prefix{Op: $op, X: $rhs}
        infix left "|" -> new Binary{L: $lhs, Op: $op, R: $rhs}
        postfix "?" -> new Postfix{X: $lhs, Op: $op}
    }
    level add { infix left "+" -> new Binary{L: $lhs, Op: $op, R: $rhs} }
    level mul { infix left "*" -> new Binary{L: $lhs, Op: $op, R: $rhs} }
    level power {
        infix right "^" -> new Binary{L: $lhs, Op: $op, R: $rhs}
        postfix "!" -> new Postfix{X: $lhs, Op: $op}
    }
}`

func TestPrattRestrictedPrefix(t *testing.T) {
	check(t, restrictedPrefix,
		ok("~é", `(Prefix Op="~" X=Atom"é"@atom)`),
		ok("~~x", `(Prefix Op="~" X=(Prefix Op="~" X=Atom"x"@atom))`),
		ok("x*y", `(Binary L=Atom"x"@atom Op="*" R=Atom"y"@atom)`),
		ok("é!", `(Postfix Op="!" X=Atom"é"@atom)`),
		ok("x^y^z", `(Binary L=Atom"x"@atom Op="^" R=(Binary L=Atom"y"@atom Op="^" R=Atom"z"@atom))`),
		// A lower-level prefix still uses its own level for its right operand.
		ok("~é+y", `(Prefix Op="~" X=(Binary L=Atom"é"@atom Op="+" R=Atom"y"@atom))`),
		ok("x*~y+z", `(Binary L=Atom"x"@atom Op="*" R=(Prefix Op="~" X=(Binary L=Atom"y"@atom Op="+" R=Atom"z"@atom)))`),
		ok("x^~y+z", `(Binary L=Atom"x"@atom Op="^" R=(Prefix Op="~" X=(Binary L=Atom"y"@atom Op="+" R=Atom"z"@atom)))`),
		// Tail operators below the active minimum are left for the caller.
		fails("x+y", "end of input"),
		fails("x|y", "end of input"),
		fails("é?", "end of input"),
		fails("~é|y", "end of input"),
		fails("~é?", "end of input"),
		fails("~", "expected"),
	)

	// A restricted lookahead and an unrestricted call at the same position must
	// not share a result with a different minimum level, even after a prefix.
	check(t, strings.Replace(restrictedPrefix, `def main = x:e(mul) $$ -> $x`,
		`def main = &(e(mul) "|") x:e $$ -> $x`, 1),
		ok("~é+y|z", `(Binary L=(Prefix Op="~" X=(Binary L=Atom"é"@atom Op="+" R=Atom"y"@atom)) Op="|" R=Atom"z"@atom)`),
		fails("~é+y", `1:1: syntax error`),
	)
}

func TestPrattCompileErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`def e = pratt { operand e "x" / "1" }`, `operand of e calls e at its start (left recursion)`},
		{`def e = pratt { operand "1" level { infix left lhs:"+" } }`, `capture name lhs is reserved in pratt operators`},
		{`def e = pratt { operand "1" level { infix left "+" -> $1 } }`, `$1 cannot be used in pratt operator actions`},
		{`def e = pratt { operand "1" level { prefix "-" -> $lhs } }`, `undefined capture $lhs`},
		{`def e = pratt { operand "1" level a { } level a { } }`, `duplicate level name a`},
		{`def e = pratt { level { prefix "-" } }`, `pratt needs at least one operand`},
		{`def main = e(nope)
def e = pratt { operand "1" }`, `rule e has no level nope`},
		{`def main = "(" pratt { operand "1" } ")"`, `syntax error`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			var got string
			if strings.Contains(tc.want, "syntax error") {
				got = "syntax error"
			} else {
				got = compileError(t, tc.src)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestPrattEmptyPrefixAndPostfix checks that prefix and postfix operators that match the empty string
// are not applied, instead of being applied forever.
func TestPrattEmptyPrefixAndPostfix(t *testing.T) {
	check(t, `
def main = e $$
def e = pratt {
    operand "x"
    level { prefix "-"? }
    level { postfix "!"? }
}`,
		ok("x", `(Seq "x"@e)@main`),
		ok("-x!", `(Seq (Operator "-" (Operator "x"@e "!" operator=1)@e operator=0)@e)@main`),
		fails("", "expected"),
	)
}

// TestPrattEmptyApplication checks that an operator application that consumes no input ends the
// loop, as an empty iteration ends a repetition: with an operand and an operator that can both
// match empty, the loop would never end.
func TestPrattEmptyApplication(t *testing.T) {
	check(t, `
def main = e $$
def e = pratt { operand "a"? level { infix left _ } }`,
		ok("a", `(Seq (Operator "a"@e "" nil operator=0)@e)@main`),
		ok("", `(Seq (Operator nil "" nil operator=0)@e)@main`))
	check(t, `
def main = e $$
def e = pratt { operand "a"? level { postfix "!"? } }`,
		ok("a!!", `(Seq (Operator (Operator "a"@e "!" operator=0)@e "!" operator=0)@e)@main`))
}
