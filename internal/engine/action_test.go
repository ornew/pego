package engine

import (
	"strings"
	"testing"
)

func TestCaptures(t *testing.T) {
	t.Run("rule level", func(t *testing.T) {
		check(t, `def main = first:word " " last:word
def word = @(?a-z)+`,
			ok("ab cd", `(Seq "ab"@word " " "cd"@word first="ab"@word last="cd"@word)@main`),
		)
	})
	t.Run("in choice and optional", func(t *testing.T) {
		check(t, `def main = (a:"a" / b:"b") c:"c"?`,
			ok("a", `(Seq "a" nil a="a")@main`),
			ok("bc", `(Seq "b" "c" b="b" c="c")@main`),
		)
	})
	t.Run("repetition scope", func(t *testing.T) {
		check(t, `def main = items:(k:@(?a-z)+ "=" v:@(?0-9)+ ";")*`,
			// A capture of the whole body is wrapped in a Seq and made a field.
			ok("a=1;b=2;", `(Seq [(Seq "a" "=" "1" ";" k="a" v="1") (Seq "b" "=" "2" ";" k="b" v="2")] items=[(Seq "a" "=" "1" ";" k="a" v="1") (Seq "b" "=" "2" ";" k="b" v="2")])@main`),
		)
	})
	t.Run("backtracking resets captures", func(t *testing.T) {
		check(t, `def main = x:"a" "b" / "a" "c" -> new R{X: $x}
type R struct { X *Match }`,
			ok("ac", `(R X=nil)`),
		)
	})
}

func TestActions(t *testing.T) {
	check(t, `
type KV struct { Key Match, Op Match, Value Match }
def main = k:@(?a-z)+ op:"=" v:@(?0-9)+ -> new KV{Key: $1, Op: $op, Value: $3}`,
		ok("abc=12", `(KV Key="abc" Op="=" Value="12")`),
	)
	check(t, `def main = "(" e:inner ")" -> $e
def inner = @(?a-z)+`,
		ok("(ab)", `"ab"@inner`),
	)
	check(t, `
type Pair struct { All node, N int, S string, B bool }
def main = "a" "b" -> new Pair{All: $0, N: 1 + 2 * 3, S: "x" + text($2), B: !false && 1 < 2}`,
		ok("ab", "(Pair All=[\"a\" \"b\"] B=true N=7 S=`xb`)"),
	)
}

// TestActionResultsAreFinal checks that a node returned by an action from its own rule body is
// not labeled by the rules that receive it. Such a node can be shared through the memo, so
// labeling it would make the result depend on which call was memoized.
func TestActionResultsAreFinal(t *testing.T) {
	src := `
def main = (a "x" / b "y")*
def a = r
def b = r
def r = c:"q" e -> $c
def e = _`
	want := `[(Seq "q" "y") (Seq "q" "x") (Seq "q" "y")]@main`
	check(t, src, ok("qyqxqy", want), ok("qy", `[(Seq "q" "y")]@main`))
	doc, err := compile(t, src).NewDocument("main", "qyqxqy")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.String() != want {
		t.Errorf("document: got %v, %v", n, err)
	}
}

// TestConcatErrorOrder checks that concat reports the same error on every backend when an
// argument that is not a list call fails late: the arguments are evaluated before concat
// checks them.
func TestConcatErrorOrder(t *testing.T) {
	check(t, `def main = [v = 1] x:"a" -> concat(v, u)`,
		ok("a", "error: action in main: variable u is not defined"))
	check(t, `
type A struct { N int }
def main = [v = 1] x:"a" -> concat(v, list(new A{N: 1 / 0}))`,
		ok("a", "error: action in main: division by zero"))
}

// TestOptionalResults checks rules and actions whose result is an optional value (*T).
func TestOptionalResults(t *testing.T) {
	check(t, `def main = x:"a"? -> $x`,
		ok("a", `"a"`),
		ok("", `nil`),
	)
	check(t, `
type C struct { X Match, Next *C }
def main: *C = xs:"a"* -> foldr(nil, $xs, (acc, x) => new C{X: $x, Next: $acc})`,
		ok("", `nil`),
		ok("aa", `(C Next=(C Next=nil X="a") X="a")`),
	)
}

func TestFold(t *testing.T) {
	src := `
type Op struct { Left Node, Op Match, Right Node }
type Node = Op | Match
def main: Node =
    l:num rest:(op:@("+" / "-") r:num)*
    -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})
def right: Node =
    l:num rest:(op:"^" r:num)*
    -> foldr($l, $rest, (acc, i) => new Op{Left: $i.r, Op: $i.op, Right: $acc})
def num = @(?0-9)+`
	check(t, src,
		ok("1", `"1"@num`),
		ok("1+2-3", `(Op Left=(Op Left="1"@num Op="+" Right="2"@num) Op="-" Right="3"@num)`),
	)
}

func TestListBuiltins(t *testing.T) {
	check(t, `
type Args struct { Items []Match, N int }
def main = first:item rest:(-"," x:item)*
    -> new Args{Items: concat(list($first), map($rest, (r) => $r.x)), N: len($rest) + 1}
def item = @(?a-z)+`,
		ok("a,bc,d", `(Args Items=["a"@item "bc"@item "d"@item] N=3)`),
	)
	// An inner lambda reads a parameter of the enclosing lambda.
	check(t, `
type Pair struct { G Match, X Match }
def main = gs:group* -> map($gs, (g) => map($g.xs, (x) => new Pair{G: $g.k, X: $x.v}))
def group = k:@(?a-z) "=" xs:(v:@(?0-9))* ";"`,
		ok("a=12;b=3;", `[[(Pair G="a" X="1") (Pair G="a" X="2")] [(Pair G="b" X="3")]]`),
	)
}

func TestMemberAccess(t *testing.T) {
	check(t, `
type Span struct { Start int, End int, Kids int }
def main = "  " w:word -> new Span{Start: $w.startPos, End: $w.endPos, Kids: len($w.children)}
def word = (?a-z) (?a-z)`,
		ok("  ab", `(Span End=4 Kids=2 Start=2)`),
	)
}

func TestNodePositions(t *testing.T) {
	prog := compile(t, `
type Op struct { Left Node, Right Node }
type Node = Op | Match
def main: Node = l:num rest:(-" "* -"+" r:num)* -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Right: $i.r})
def num = -" "* n:@(?0-9)+ -> $n`)
	n, err := prog.Parse("main", " 1 + 22+3")
	if err != nil {
		t.Fatal(err)
	}
	// The final result has the range of the whole rule; intermediate nodes have the ranges of their
	// fields.
	if n.Start != 0 || n.End != 9 {
		t.Errorf("root [%d,%d)", n.Start, n.End)
	}
	left := n.Field("Left").(*Node)
	if left.Start != 1 || left.End != 7 {
		t.Errorf("left [%d,%d)", left.Start, left.End)
	}
	if r := left.Field("Right").(*Node); r.Start != 5 || r.End != 7 || r.Text != "22" {
		t.Errorf("22 at [%d,%d) %q", r.Start, r.End, r.Text)
	}
}

func TestPredicates(t *testing.T) {
	t.Run("comparison", func(t *testing.T) {
		check(t, `def main = d:@(?0-9)+ [len($d) <= 3] "!"`,
			ok("123!", `(Seq "123" "!" d="123")@main`),
			fails("1234!", `1:5: syntax error: expected (?0-9)`),
		)
	})
	t.Run("variables are scoped to rules", func(t *testing.T) {
		check(t, `
def main = [x = 1] a [x == 1]
def a = [x == 1] [x = 2] [x == 2] "a"`,
			ok("a", `(Seq (Seq "a")@a)@main`),
		)
	})
	t.Run("undefined variable fails", func(t *testing.T) {
		check(t, `def main = [y > 0] "a" / "b"`,
			ok("b", `"b"@main`),
			fails("a", `expected "b"`),
		)
	})
	t.Run("backtracking rolls back definitions", func(t *testing.T) {
		check(t, `def main = ([x = 1] "a" "b" / "a") [x == 1] "c"`,
			fails("ac", `1:2: syntax error: expected "b"`),
			ok("abc", `(Seq (Seq "a" "b") "c")@main`),
		)
	})
	t.Run("indentation", func(t *testing.T) {
		src := `
def main = [indent = 0] block $$
def block = item+
def item = s:spaces [len($s) == indent] name:@(?a-z)+ "\n" children?
def children = &(s:spaces) [len($s) > indent] [indent = len($s)] block
def spaces = @" "*`
		check(t, src,
			ok(lines("a", "  b", "  c", "    d", "e", ""),
				`(Seq [(Seq ""@spaces "a" "\n" (Seq [(Seq "  "@spaces "b" "\n" nil name="b" s="  "@spaces)@item (Seq "  "@spaces "c" "\n" (Seq [(Seq "    "@spaces "d" "\n" nil name="d" s="    "@spaces)@item]@block s="    "@spaces)@children name="c" s="  "@spaces)@item]@block s="  "@spaces)@children name="a" s=""@spaces)@item (Seq ""@spaces "e" "\n" nil name="e" s=""@spaces)@item]@block)@main`),
			// The indentation of " c" matches no indentation level.
			fails(lines("a", "   b", " c", ""), `3:2: syntax error: expected " "`),
		)
	})
}

func TestCompileErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`def main = x`, `1:12: undefined rule x`},
		{`def main = "a"
def main = "b"`, `2:1: rule main is already defined`},
		{`type Match terminal`, `type Match is reserved`},
		{`type foo terminal
def main = "a"`, `type foo must start with an uppercase letter`},
		{`type Foo struct { bar Match }
def main = "a"`, `field bar of Foo must start with an uppercase letter`},
		{`type A terminal
type A terminal`, `type A is already defined`},
		{`def main = "a" -> $x`, `1:19: undefined capture $x`},
		{`def main = "a" -> new Nope{}`, `Nope is not a struct type`},
		{`type A struct { X Match }
def main = "a" -> new A{Y: $1}`, `A has no field Y`},
		{`type A struct { X Match }
def main = "a" -> new A{X: $1, X: $1}`, `duplicate field X`},
		{`def main = "a" -> nope(1)`, `unknown function nope`},
		{`def main = "a" -> len(1, 2)`, `len takes 1 arguments, got 2`},
		{`def main = @(x:"a")`, `capture x inside @, -, or ! has no effect`},
		{`def main = x:-"a"`, `capture x of an expression without a value`},
		{`def main = "a" #nope`, `unknown attribute #nope`},
		// A repetition element sees only its own captures, whether it has any or not.
		{`def main = n:"a" ([$n != nil] "x")* "y"`, `1:20: capture $n is not visible in a repetition element`},
		{`def main = n:"a" (m:"x" [$n != nil])* "y"`, `1:26: capture $n is not visible in a repetition element`},
		{`def main = x(lvl)
def x = "a"`, `rule x has no pratt levels`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestRuntimeErrors(t *testing.T) {
	prog := compile(t, `
type A struct { N int }
def main = "a" -> new A{N: 1 / (len($1) - 1)}`)
	_, err := prog.Parse("main", "a")
	if err == nil || !strings.Contains(err.Error(), "action in main: division by zero") {
		t.Errorf("got %v", err)
	}
	if _, err := prog.Parse("nope", "a"); err == nil || err.Error() != "rule nope is not defined" {
		t.Errorf("got %v", err)
	}
}

// TestTextOfNil checks that text of a capture that did not match is "" on every backend, as
// specified.
func TestTextOfNil(t *testing.T) {
	check(t, `def main = x:"a"? [text($x) == ""] "b"`, ok("b", `(Seq nil "b")@main`))
	check(t, `
type T struct { S string }
def main = x:"a"? "b" -> new T{S: text($x) + "!"}`, ok("b", "(T S=`!`)"))
}

// TestLargeIntegers checks that integer literals beyond 32 bits keep their value on every backend.
func TestLargeIntegers(t *testing.T) {
	prog := compile(t, `
type T struct { A int, B int, C int, D int, E int }
def main = "a" -> new T{A: 3000000000, B: 0 - 3000000000, C: 9223372036854775807, D: 0 - 9223372036854775807 - 1, E: 4294967296}`)
	want := `(T A=3000000000 B=-3000000000 C=9223372036854775807 D=-9223372036854775808 E=4294967296)`
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if n, err := prog.ParseWith("main", "a", ParseOptions{Backend: b}); err != nil || n.String() != want {
			t.Errorf("%v: got %v, %v, want %s", b, n, err, want)
		}
	}
}

// TestLenOfStrings checks that len counts the characters of strings in the position unit on
// every backend, for strings that do not come from the input too.
func TestLenOfStrings(t *testing.T) {
	prog := compile(t, `
type T struct { N int, M int }
def main = x:"a" -> new T{N: len("日本"), M: len(text($x) + "é")}`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, tc := range []struct {
			o    ParseOptions
			want string
		}{
			{ParseOptions{Backend: b}, `(T M=2 N=2)`},
			{ParseOptions{Backend: b, Unit: Bytes}, `(T M=3 N=6)`},
		} {
			if n, err := prog.ParseWith("main", "a", tc.o); err != nil || n.String() != tc.want {
				t.Errorf("%v %v: got %v, %v, want %s", b, tc.o.Unit, n, err, tc.want)
			}
		}
	}
}
