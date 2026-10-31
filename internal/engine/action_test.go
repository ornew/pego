package engine

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/gentest"
	"github.com/ornew/zeroparse/internal/value"
)

func TestCaptures(t *testing.T) {
	t.Parallel()
	t.Run("rule level", func(t *testing.T) {
		check(t, `rule main = first:word " " last:word
rule word = [a-z]+ #atomic`,
			ok("ab cd", `(first:"ab"@word " " last:"cd"@word)@main`),
		)
	})
	t.Run("in choice and optional", func(t *testing.T) {
		check(t, `rule main = (a:"a" / b:"b") c:"c"?`,
			ok("a", `(a:"a" nil)@main`),
			ok("bc", `(b:"b" c:"c")@main`),
		)
	})
	t.Run("repetition scope", func(t *testing.T) {
		check(t, `rule main = items:(k:[a-z]+ #atomic "=" v:[0-9]+ #atomic ";")*`,
			// A capture of the whole body is wrapped in a Seq and made a field.
			ok("a=1;b=2;", `(items:[(k:"a" "=" v:"1" ";") (k:"b" "=" v:"2" ";")])@main`),
		)
	})
	t.Run("capture of an expression without a value", func(t *testing.T) {
		// The capture's value is the unit tuple of the range matched.
		check(t, `type T = { S string; N int }
rule main = x:-"a" "b" => T{S: text($x), N: len($x.Value)}`, ok("ab", `T{N: 0, S: "a"}`))
		check(t, `rule main = x:-"a"`, ok("a", `(x:())@main`))
	})
	t.Run("backtracking resets captures", func(t *testing.T) {
		check(t, `rule main = x:"a" "b" / "a" "c" => R{X: $x}
type R = { X *node[string] }`,
			ok("ac", `R{X: nil}`),
		)
	})
}

func TestActions(t *testing.T) {
	t.Parallel()
	check(t, `type KV = { Key node[string]; Op node[string]; Value node[string] }
rule main = k:[a-z]+ #atomic op:"=" v:[0-9]+ #atomic => KV{Key: $k, Op: $op, Value: $v}`,
		ok("abc=12", `KV{Key: "abc", Op: "=", Value: "12"}`),
	)
	check(t, `rule main = "(" e:inner ")" => $e
rule inner = [a-z]+ #atomic`,
		ok("(ab)", `"ab"@inner`),
	)
	check(t, `type Pair = { All []node[string]; N int; S string; B bool }
rule main = v1:"a" v2:"b" => Pair{All: list($v1, $v2), N: 1 + 2 * 3, S: "x" + text($v2), B: !false && 1 < 2}`,
		ok("ab", `Pair{All: ["a" "b"], B: true, N: 7, S: "xb"}`),
	)
}

// TestActionResultsAreFinal checks that a node returned by an action from its own rule body is
// not labeled by the rules that receive it. Such a node can be shared through the memo, so
// labeling it would make the result depend on which call was memoized.
func TestActionResultsAreFinal(t *testing.T) {
	t.Parallel()
	src := `rule main = (a "x" / b "y")*
rule a = r
rule b = r
rule r = c:"q" e => $c
rule e = _`
	want := `[("q" "y") ("q" "x") ("q" "y")]@main`
	check(t, src, ok("qyqxqy", want), ok("qy", `[("q" "y")]@main`))
	doc, err := mustCompile(t, src).NewDocument("main", "qyqxqy")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := doc.Parse(); err != nil || n.String() != want {
		t.Errorf("document: got %v, %v", n, err)
	}
}

// TestOptionalResults checks rules and actions whose result is an optional value (*T).
func TestOptionalResults(t *testing.T) {
	t.Parallel()
	check(t, `rule main = x:"a"? => $x`,
		ok("a", `"a"`),
		ok("", `nil`),
	)
	check(t, `type C = { X node[string]; Next *C }
rule main *C = xs:"a"* => foldr(nil, $xs, (acc, x) => C{X: x, Next: acc})`,
		ok("", `nil`),
		ok("aa", `C{Next: C{Next: nil, X: "a"}, X: "a"}`),
	)
}

func TestFold(t *testing.T) {
	t.Parallel()
	src := `type Op = { Left Node; Op node[string]; Right Node }
type Node ~ Op | node[string]
rule main Node = (
    l:num rest:(op:("+" / "-") #atomic r:num)*
) => foldl($l, $rest, (acc, i) => Op{Left: acc, Op: i.$op, Right: i.$r})
rule right Node = (
    l:num rest:(op:"^" r:num)*
) => foldr($l, $rest, (acc, i) => Op{Left: i.$r, Op: i.$op, Right: acc})
rule num = [0-9]+ #atomic`
	check(t, src,
		ok("1", `"1"@num`),
		ok("1+2-3", `Op{Left: Op{Left: "1"@num, Op: "+", Right: "2"@num}, Op: "-", Right: "3"@num}`),
	)
}

func TestListBuiltins(t *testing.T) {
	t.Parallel()
	check(t, `type Args = { Items []node[string]; N int }
rule main = (
    first:item rest:(-"," x:item)*
) => Args{Items: concat(list($first), map($rest, (r) => r.$x)), N: len($rest) + 1}
rule item = [a-z]+ #atomic`,
		ok("a,bc,d", `Args{Items: ["a"@item "bc"@item "d"@item], N: 3}`),
	)
	// An inner lambda reads a parameter of the enclosing lambda.
	check(t, `type Pair = { G node[string]; X node[string] }
rule main = gs:group* => map($gs, (g) => map(g.$xs, (x) => Pair{G: g.$k, X: x.$v}))
rule group = k:[a-z] #atomic "=" xs:(v:[0-9] #atomic)* ";"`,
		ok("a=12;b=3;", `[[Pair{G: "a", X: "1"} Pair{G: "a", X: "2"}] [Pair{G: "b", X: "3"}]]`),
	)
}

func TestMemberAccess(t *testing.T) {
	t.Parallel()
	check(t, `type Span = { Start int; End int; Kids int }
rule main = "  " w:word => Span{Start: $w.Start, End: $w.End, Kids: len($w.Value)}
rule word = [a-z] [a-z]`,
		ok("  ab", `Span{End: 4, Kids: 2, Start: 2}`),
	)
}

func TestNodePositions(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type Op = { Left Node; Right Node }
type Node ~ Op | node[string]
rule main Node = l:num rest:(-" "* -"+" r:num)* => foldl($l, $rest, (acc, i) => Op{Left: acc, Right: i.$r})
rule num = -" "* n:[0-9]+ #atomic => $n`)
	n, err := prog.Parse("main", " 1 + 22+3")
	if err != nil {
		t.Fatal(err)
	}
	// The final result has the range of the whole rule; intermediate nodes have the ranges of their
	// fields.
	if value.D(n).Start != 0 || value.D(n).End != 9 {
		t.Errorf("root [%d,%d)", value.D(n).Start, value.D(n).End)
	}
	left := n.Field("Left").(*value.Node)
	if value.D(left).Start != 1 || value.D(left).End != 7 {
		t.Errorf("left [%d,%d)", value.D(left).Start, value.D(left).End)
	}
	if r := left.Field("Right").(*value.Node); value.D(r).Start != 5 || value.D(r).End != 7 || value.D(r).Text != "22" {
		t.Errorf("22 at [%d,%d) %q", value.D(r).Start, value.D(r).End, value.D(r).Text)
	}
}

func TestPredicates(t *testing.T) {
	t.Parallel()
	t.Run("comparison", func(t *testing.T) {
		check(t, `rule main = d:[0-9]+ #atomic {len($d) <= 3} "!"`,
			ok("123!", `(d:"123" "!")@main`),
			fails("1234!", `1:5: syntax error: expected [0-9]`),
		)
	})
	t.Run("variables are scoped to rules", func(t *testing.T) {
		check(t, `var x int
rule main = {x := 1} a {x == 1}
rule a = {x == 1} {x := 2} {x == 2} "a"`,
			ok("a", `(("a")@a)@main`),
		)
	})
	t.Run("unbound variable has its initial value", func(t *testing.T) {
		check(t, `var y int
rule main = {y > 0} "a" / "b"`,
			ok("b", `"b"@main`),
			fails("a", `expected "b"`),
		)
	})
	t.Run("backtracking rolls back definitions", func(t *testing.T) {
		check(t, `var x int
rule main = ({x := 1} "a" "b" / "a") {x == 1} "c"`,
			fails("ac", `1:2: syntax error: expected "b"`),
			ok("abc", `(("a" "b") "c")@main`),
		)
	})
	t.Run("indentation", func(t *testing.T) {
		src := `var indent int
rule main = {indent := 0} block $$
rule block = item+
rule item = s:spaces {len($s) == indent} name:[a-z]+ #atomic "\n" children?
rule children = &s:spaces {len($s) > indent} {indent := len($s)} block
rule spaces = " "* #atomic`
		check(t, src,
			ok(lines("a", "  b", "  c", "    d", "e", ""),
				`([(s:""@spaces name:"a" "\n" ([(s:"  "@spaces name:"b" "\n" nil)@item (s:"  "@spaces name:"c" "\n" ([(s:"    "@spaces name:"d" "\n" nil)@item]@block &s:"    "@spaces)@children)@item]@block &s:"  "@spaces)@children)@item (s:""@spaces name:"e" "\n" nil)@item]@block)@main`),
			// The indentation of " c" matches no indentation level.
			fails(lines("a", "   b", " c", ""), `3:2: syntax error: expected " "`),
		)
	})
}

func TestRuntimeErrors(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type A = { N int }
rule main = v1:"a" => A{N: 1 / (len($v1) - 1)}`)
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
	t.Parallel()
	check(t, `rule main = x:"a"? {text($x) == ""} "b"`, ok("b", `(nil "b")@main`))
	check(t, `type T = { S string }
rule main = x:"a"? "b" => T{S: text($x) + "!"}`, ok("b", `T{S: "!"}`))
}

// TestLargeIntegers checks that integer literals beyond 32 bits keep their value on every backend.
func TestLargeIntegers(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type T = { A int; B int; C int; D int; E int }
rule main = "a" => T{A: 3000000000, B: 0 - 3000000000, C: 9223372036854775807, D: 0 - 9223372036854775807 - 1, E: 4294967296}`)
	want := `T{A: 3000000000, B: -3000000000, C: 9223372036854775807, D: -9223372036854775808, E: 4294967296}`
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		if n, err := prog.ParseWith("main", "a", ParseOptions{Backend: b}); err != nil || n.String() != want {
			t.Errorf("%v: got %v, %v, want %s", b, n, err, want)
		}
	}
}

// TestIntegerWrapAround checks that integer arithmetic wraps around in 64 bits
// with Go's results on every backend, including the most negative integer
// divided by -1, and that dividing by zero is a runtime error.
func TestIntegerWrapAround(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.IntegerBoundaryGrammar)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, n := range []int64{1, 2} {
			maxInt, minInt := int64(math.MaxInt64), int64(math.MinInt64)
			want := fmt.Sprintf("I{Add: %d, Big: %d, Mul: %d, Neg: %d, Quo: %d, Rem: %d, Sub: %d}",
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

// TestActionEnvironments pins the variable environment that actions see on
// every backend; the shared generated corpus compares the other parsers with
// these results.
func TestActionEnvironments(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, gentest.ActionEnvironmentGrammar)
	// peek calls plain at the start twice with another depth, so batch parsers
	// memoize it there before main calls it.
	if r := prog.ByName["plain"]; !r.Memo || r.Transient || len(r.Vars) != 2 {
		t.Fatal("plain must be memoized by its two variables")
	}
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, tc := range []struct{ input, want string }{
			// The values peek defined do not reach main's calls, and its
			// definitions are visible to its own calls.
			{"a", `Out{Count: 1, Items: [Item{Depth: 0, Name: "a"@name, Note: nil, Size: 1, Tag: "top"}], Sum: 1, Tag: "top"}`},
			{"a%b", `Out{Count: 1, Items: [Item{Depth: 0, Name: "b"@name, Note: nil, Size: 1, Tag: "top"}], Sum: 1, Tag: "top"}`},
			// Callers' variables reach nested calls.
			{"(((a)))", `Out{Count: 1, Items: [Item{Depth: 3, Name: "a"@name, Note: nil, Size: 4, Tag: "top"}], Sum: 34, Tag: "top"}`},
			// A shadowing definition is visible only below its invocation.
			{"a(#t:b)c?", `Out{Count: 3, Items: [Item{Depth: 0, Name: "a"@name, Note: nil, Size: 1, Tag: "top"} Item{Depth: 1, Name: "b"@name, Note: nil, Size: 2, Tag: "top/t"} Item{Depth: 0, Name: "c"@name, Note: "?", Size: 1, Tag: "top"}], Sum: 14, Tag: "top"}`},
			// A variable that nothing has bound has its initial value, which fails
			// deep's predicate unless the left operand of || short-circuits it.
			{"((b))", `Out{Count: 1, Items: [Item{Depth: 2, Name: "b"@name, Note: nil, Size: 3, Tag: "top"}], Sum: 23, Tag: "top"}`},
			{"#x:((b))", `Out{Count: 1, Items: [Item{Depth: 2, Name: "b"@name, Size: 100, Tag: "top/x"}], Sum: 120, Tag: "top"}`},
			// shout's definition is rolled back when it fails.
			{"a!!", `Out{Count: 1, Items: [Item{Depth: 0, Name: "a"@name, Note: "!", Size: -1, Tag: "shadow"}], Sum: -1, Tag: "top"}`},
			{"ab?", `Out{Count: 1, Items: [Item{Depth: 0, Name: "ab"@name, Note: "?", Size: 2, Tag: "top"}], Sum: 2, Tag: "top"}`},
			// The last repetition element's definition remains.
			{"[a,bc]", `Out{Count: 1, Items: [Item{Depth: 0, Name: "]", Size: 2, Tag: "bc"}], Sum: 2, Tag: "top"}`},
		} {
			if got, err := prog.ParseWith("main", tc.input, ParseOptions{Backend: b}); err != nil || got.String() != tc.want {
				t.Errorf("%v %q: got %v, %v\nwant %s", b, tc.input, got, err, tc.want)
			}
		}
		// A variable that nothing has bound reads as its initial value.
		for _, tc := range []struct{ input, want string }{
			{"~a", `Out{Count: 1, Items: [Item{Depth: 0, Name: "a"@name, Size: 0, Tag: "top"}], Sum: 0, Tag: "top"}`},
			{"[]", `Out{Count: 1, Items: [Item{Depth: 0, Name: "]", Size: 0, Tag: ""}], Sum: 0, Tag: "top"}`},
		} {
			if got, err := prog.ParseWith("main", tc.input, ParseOptions{Backend: b}); err != nil || got.String() != tc.want {
				t.Errorf("%v %q: got %v, %v\nwant %s", b, tc.input, got, err, tc.want)
			}
		}
	}
}

// TestLenOfStrings checks that len counts the characters of strings in the position unit on
// every backend, for strings that do not come from the input too.
func TestLenOfStrings(t *testing.T) {
	t.Parallel()
	prog := mustCompile(t, `type T = { N int; M int }
rule main = x:"a" => T{N: len("日本"), M: len(text($x) + "é")}`)
	for _, b := range []Backend{Closure, Bytecode, BytecodeIterative} {
		for _, tc := range []struct {
			o    ParseOptions
			want string
		}{
			{ParseOptions{Backend: b}, `T{M: 2, N: 2}`},
			{ParseOptions{Backend: b, Unit: Bytes}, `T{M: 3, N: 6}`},
		} {
			if n, err := prog.ParseWith("main", "a", tc.o); err != nil || n.String() != tc.want {
				t.Errorf("%v %v: got %v, %v, want %s", b, tc.o.Unit, n, err, tc.want)
			}
		}
	}
}
