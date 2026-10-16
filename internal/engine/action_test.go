package engine

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestCaptures(t *testing.T) {
	t.Run("rule level", func(t *testing.T) {
		check(t, `rule main = first:word " " last:word
rule word = [a-z]+ #atomic`,
			ok("ab cd", `(Seq "ab"@word " " "cd"@word first="ab"@word last="cd"@word)@main`),
		)
	})
	t.Run("in choice and optional", func(t *testing.T) {
		check(t, `rule main = (a:"a" / b:"b") c:"c"?`,
			ok("a", `(Seq "a" nil a="a")@main`),
			ok("bc", `(Seq "b" "c" b="b" c="c")@main`),
		)
	})
	t.Run("repetition scope", func(t *testing.T) {
		check(t, `rule main = items:(k:[a-z]+ #atomic "=" v:[0-9]+ #atomic ";")*`,
			// A capture of the whole body is wrapped in a Seq and made a field.
			ok("a=1;b=2;", `(Seq [(Seq "a" "=" "1" ";" k="a" v="1") (Seq "b" "=" "2" ";" k="b" v="2")] items=[(Seq "a" "=" "1" ";" k="a" v="1") (Seq "b" "=" "2" ";" k="b" v="2")])@main`),
		)
	})
	t.Run("backtracking resets captures", func(t *testing.T) {
		check(t, `rule main = x:"a" "b" / "a" "c" => R{X: $x}
type R struct { X *Match }`,
			ok("ac", `(R X=nil)`),
		)
	})
}

func TestActions(t *testing.T) {
	check(t, `type KV struct { Key Match; Op Match; Value Match }
rule main = k:[a-z]+ #atomic op:"=" v:[0-9]+ #atomic => KV{Key: $1, Op: $op, Value: $3}`,
		ok("abc=12", `(KV Key="abc" Op="=" Value="12")`),
	)
	check(t, `rule main = "(" e:inner ")" => $e
rule inner = [a-z]+ #atomic`,
		ok("(ab)", `"ab"@inner`),
	)
	check(t, `type Pair struct { All node; N int; S string; B bool }
rule main = "a" "b" => Pair{All: $0, N: 1 + 2 * 3, S: "x" + text($2), B: !false && 1 < 2}`,
		ok("ab", "(Pair All=[\"a\" \"b\"] B=true N=7 S=`xb`)"),
	)
}

// TestActionResultsAreFinal checks that a node returned by an action from its own rule body is
// not labeled by the rules that receive it. Such a node can be shared through the memo, so
// labeling it would make the result depend on which call was memoized.
func TestActionResultsAreFinal(t *testing.T) {
	src := `rule main = (a "x" / b "y")*
rule a = r
rule b = r
rule r = c:"q" e => $c
rule e = _`
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
	check(t, `rule main = {v := 1} x:"a" => concat(v, u)`,
		ok("a", "error: action in main: variable u is not defined"))
	check(t, `type A struct { N int }
rule main = {v := 1} x:"a" => concat(v, list(A{N: 1 / 0}))`,
		ok("a", "error: action in main: division by zero"))
}

// TestOptionalResults checks rules and actions whose result is an optional value (*T).
func TestOptionalResults(t *testing.T) {
	check(t, `rule main = x:"a"? => $x`,
		ok("a", `"a"`),
		ok("", `nil`),
	)
	check(t, `type C struct { X Match; Next *C }
rule main *C = xs:"a"* => foldr(nil, $xs, (acc, x) => C{X: $x, Next: $acc})`,
		ok("", `nil`),
		ok("aa", `(C Next=(C Next=nil X="a") X="a")`),
	)
}

func TestFold(t *testing.T) {
	src := `type Op struct { Left Node; Op Match; Right Node }
type Node = Op | Match
rule main Node = (
    l:num rest:(op:("+" / "-") #atomic r:num)*
) => foldl($l, $rest, (acc, i) => Op{Left: $acc, Op: $i.op, Right: $i.r})
rule right Node = (
    l:num rest:(op:"^" r:num)*
) => foldr($l, $rest, (acc, i) => Op{Left: $i.r, Op: $i.op, Right: $acc})
rule num = [0-9]+ #atomic`
	check(t, src,
		ok("1", `"1"@num`),
		ok("1+2-3", `(Op Left=(Op Left="1"@num Op="+" Right="2"@num) Op="-" Right="3"@num)`),
	)
}

func TestListBuiltins(t *testing.T) {
	check(t, `type Args struct { Items []Match; N int }
rule main = (
    first:item rest:(-"," x:item)*
) => Args{Items: concat(list($first), map($rest, (r) => $r.x)), N: len($rest) + 1}
rule item = [a-z]+ #atomic`,
		ok("a,bc,d", `(Args Items=["a"@item "bc"@item "d"@item] N=3)`),
	)
	// An inner lambda reads a parameter of the enclosing lambda.
	check(t, `type Pair struct { G Match; X Match }
rule main = gs:group* => map($gs, (g) => map($g.xs, (x) => Pair{G: $g.k, X: $x.v}))
rule group = k:[a-z] #atomic "=" xs:(v:[0-9] #atomic)* ";"`,
		ok("a=12;b=3;", `[[(Pair G="a" X="1") (Pair G="a" X="2")] [(Pair G="b" X="3")]]`),
	)
}

func TestMemberAccess(t *testing.T) {
	check(t, `type Span struct { Start int; End int; Kids int }
rule main = "  " w:word => Span{Start: $w.startPos, End: $w.endPos, Kids: len($w.children)}
rule word = [a-z] [a-z]`,
		ok("  ab", `(Span End=4 Kids=2 Start=2)`),
	)
}

func TestNodePositions(t *testing.T) {
	prog := compile(t, `type Op struct { Left Node; Right Node }
type Node = Op | Match
rule main Node = l:num rest:(-" "* -"+" r:num)* => foldl($l, $rest, (acc, i) => Op{Left: $acc, Right: $i.r})
rule num = -" "* n:[0-9]+ #atomic => $n`)
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
		check(t, `rule main = d:[0-9]+ #atomic {len($d) <= 3} "!"`,
			ok("123!", `(Seq "123" "!" d="123")@main`),
			fails("1234!", `1:5: syntax error: expected [0-9]`),
		)
	})
	t.Run("variables are scoped to rules", func(t *testing.T) {
		check(t, `rule main = {x := 1} a {x == 1}
rule a = {x == 1} {x := 2} {x == 2} "a"`,
			ok("a", `(Seq (Seq "a")@a)@main`),
		)
	})
	t.Run("undefined variable fails", func(t *testing.T) {
		check(t, `rule main = {y > 0} "a" / "b"`,
			ok("b", `"b"@main`),
			fails("a", `expected "b"`),
		)
	})
	t.Run("backtracking rolls back definitions", func(t *testing.T) {
		check(t, `rule main = ({x := 1} "a" "b" / "a") {x == 1} "c"`,
			fails("ac", `1:2: syntax error: expected "b"`),
			ok("abc", `(Seq (Seq "a" "b") "c")@main`),
		)
	})
	t.Run("indentation", func(t *testing.T) {
		src := `rule main = {indent := 0} block $$
rule block = item+
rule item = s:spaces {len($s) == indent} name:[a-z]+ #atomic "\n" children?
rule children = &s:spaces {len($s) > indent} {indent := len($s)} block
rule spaces = " "* #atomic`
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
		{`rule main = x`, `1:13: undefined rule x`},
		{`rule main = "a"
rule main = "b"`, `2:1: rule main is already defined`},
		{`type Match terminal`, `type Match is reserved`},
		{`type foo terminal
rule main = "a"`, `type foo must start with an uppercase letter`},
		{`type Foo struct { bar Match }
rule main = "a"`, `field bar of Foo must start with an uppercase letter`},
		{`type A terminal
type A terminal`, `type A is already defined`},
		{`rule main = "a" => $x`, `1:20: undefined capture $x`},
		{`rule main = "a" => Nope{}`, `Nope is not a struct type`},
		{`type A struct { X Match }
rule main = "a" => A{Y: $1}`, `A has no field Y`},
		{`type A struct { X Match }
rule main = "a" => A{X: $1, X: $1}`, `duplicate field X`},
		{`rule main = "a" => nope(1)`, `unknown function nope`},
		{`rule main = "a" => len(1, 2)`, `len takes 1 arguments, got 2`},
		{`rule main = (x:"a") #atomic`, `capture x inside #atomic, -, or ! has no effect`},
		{`rule main = x:-"a"`, `capture x of an expression without a value`},
		{`rule main = "a" @nope`, `unknown attribute @nope`},
		// A repetition element sees only its own captures, whether it has any or not.
		{`rule main = n:"a" ({$n != nil} "x")* "y"`, `1:21: capture $n is not visible in a repetition element`},
		{`rule main = n:"a" (m:"x" {$n != nil})* "y"`, `1:27: capture $n is not visible in a repetition element`},
		{`rule main = level(x, lvl)
rule x = "a"`, `rule x has no Operators Rule levels`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestRuntimeErrors(t *testing.T) {
	prog := compile(t, `type A struct { N int }
rule main = "a" => A{N: 1 / (len($1) - 1)}`)
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
	check(t, `rule main = x:"a"? {text($x) == ""} "b"`, ok("b", `(Seq nil "b")@main`))
	check(t, `type T struct { S string }
rule main = x:"a"? "b" => T{S: text($x) + "!"}`, ok("b", "(T S=`!`)"))
}

// TestLargeIntegers checks that integer literals beyond 32 bits keep their value on every backend.
func TestLargeIntegers(t *testing.T) {
	prog := compile(t, `type T struct { A int; B int; C int; D int; E int }
rule main = "a" => T{A: 3000000000, B: 0 - 3000000000, C: 9223372036854775807, D: 0 - 9223372036854775807 - 1, E: 4294967296}`)
	want := `(T A=3000000000 B=-3000000000 C=9223372036854775807 D=-9223372036854775808 E=4294967296)`
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if n, err := prog.ParseWith("main", "a", ParseOptions{Backend: b}); err != nil || n.String() != want {
			t.Errorf("%v: got %v, %v, want %s", b, n, err, want)
		}
	}
}

// integerBoundaryGrammar computes integers beyond the 64-bit range from the
// length of the input, so that they are not constants.
const integerBoundaryGrammar = `type I struct { Add int; Sub int; Mul int; Neg int; Quo int; Rem int; Big int }
rule main = x:"a"* #atomic => I{
    Add: 9223372036854775807 + len($x),
    Sub: 0 - 9223372036854775807 - 1 - len($x),
    Mul: 9223372036854775807 * (len($x) + 1),
    Neg: -(0 - 9223372036854775807 - len($x)),
    Quo: (0 - 9223372036854775807 - 1) / (0 - len($x)),
    Rem: (0 - 9223372036854775807 - 1) % (0 - len($x)),
    Big: 9223372036854775807 - len($x),
}`

// TestIntegerWrapAround checks that integer arithmetic wraps around in 64 bits
// with Go's results on every backend, including the most negative integer
// divided by -1, and that dividing by zero is a runtime error.
func TestIntegerWrapAround(t *testing.T) {
	prog := compile(t, integerBoundaryGrammar)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, n := range []int64{1, 2} {
			maxInt, minInt := int64(math.MaxInt64), int64(math.MinInt64)
			want := fmt.Sprintf("(I Add=%d Big=%d Mul=%d Neg=%d Quo=%d Rem=%d Sub=%d)",
				maxInt+n, maxInt-n, maxInt*(n+1), -(-maxInt - n), minInt/-n, minInt%-n, minInt-n)
			if got, err := prog.ParseWith("main", strings.Repeat("a", int(n)), ParseOptions{Backend: b}); err != nil || got.String() != want {
				t.Errorf("%v, %d: got %v, %v, want %s", b, n, got, err, want)
			}
		}
		if got, err := prog.ParseWith("main", "", ParseOptions{Backend: b}); err == nil || !strings.Contains(err.Error(), "division by zero") {
			t.Errorf("%v: dividing by zero gave %v, %v", b, got, err)
		}
	}
}

// TestLenOfStrings checks that len counts the characters of strings in the position unit on
// every backend, for strings that do not come from the input too.
func TestLenOfStrings(t *testing.T) {
	prog := compile(t, `type T struct { N int; M int }
rule main = x:"a" => T{N: len("日本"), M: len(text($x) + "é")}`)
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
