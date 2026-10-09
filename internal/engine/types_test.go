package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/ornew/pego/internal/syntax"
)

func TestTypeErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`type A struct { X Nope }`, `1:19: undefined type Nope`},
		{`type A = B
type B = A
def main = "a"`, `type A refers to itself`},
		{`type P struct { X Match }
type Alias = P
def main = "a" -> new Alias{X: 1}`, `cannot use int as Match in field X of Alias`},
		{`type P struct { X Match }
type Alias = P
def main = "a" -> new Alias{Y: $1}`, `Alias has no field Y`},
		{`type P struct {}
type Q struct {}
type Alias = P | Q
def main = "a" -> new Alias{}`, `Alias is not a struct type`},
		{`type P struct {}
type Alias = *P
def main = "a" -> new Alias{}`, `Alias is not a struct type`},
		{`type A = B
type B = A
def main = "a" -> new A{}`, `A is not a struct type`},
		{`type A struct { X Match, X Match }`, `duplicate field X in A`},
		{`def main: int = "a"`, `rule main must produce a node, but its type is int`},
		{`type P struct { X Match }
def main = "a" -> new P{X: 1}`, `cannot use int as Match in field X of P`},
		{`type Num terminal
type P struct { N Num }
def main = n:@"1" -> new P{N: $n}`, `cannot use Match as Num in field N of P`},
		{`type P struct { X Match }
def main = x:"a"? -> new P{X: $x}`, `cannot use *Match as Match in field X of P`},
		{`type P struct { X Match }
def main = (x:"a" / "b") -> new P{X: $x}`, `cannot use *Match as Match in field X of P`},
		{`def main = x:("a" y:"b")? -> $x.y`, `cannot access .y of *Seq: the value may be nil`},
		{`def main = xs:(k:"a")* -> $xs.k`, `[]Seq{k: Match} has no field k`},
		{`def main = "a" "b" -> $3`, `$3 is out of range: the rule has 2 elements with a value`},
		{`def main = "a" -> len(1 + "a")`, `invalid operands int and string for +`},
		{`def main = "a" -> len(1)`, `len: invalid argument int`},
		{`def main = "a" -> 1`, `action of main must produce a node, got int`},
		{`def main = [x = $1] "a"
def b = "b"`, `1:17: $1 cannot be used in predicates`},
		{`def main = "a" [x = (y) => 1]`, `functions can only be passed to foldl, foldr, or map`},
		{`type Op struct { X Match }
def main: Op = "a"`, `rule main produces Match, which is not assignable to Op`},
		{`type E struct { X Match }
def e: E = pratt { operand "1" -> new E{X: $1} level { infix left "+" } }`, `operators of e without an action produce Operator, which is not assignable to E`},
		{`def main = "a" -> foldl($1, $1, (a, b) => $a)`, `foldl: expected a list, got Match`},
		{`def main = xs:"a"* -> map($xs, (x) => len($x))`, `map: the function must produce a node, got int`},
		{`def main = "a" -> list(1)`, `list: elements must be nodes, got int`},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := compileError(t, tc.src); !strings.Contains(got, tc.want) {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestWellTyped checks that well-typed grammars compile.
func TestWellTyped(t *testing.T) {
	for _, src := range []string{
		// Union types and the type of the foldl accumulator
		`type Number terminal
type Op struct { Left Node, Op Match, Right Node }
type Node = Op | Number
def expr: Node = l:num rest:(op:@("+" / "-") r:num)* -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})
def num: Number = (?0-9)+`,
		// The accumulator type is widened by inference
		`type Op struct { L node, R Match }
def expr = l:@"x" rest:(r:@"y")* -> foldl($l, $rest, (acc, i) => new Op{L: $acc, R: $i.r})
def main = e:expr -> $e`,
		// Inferred rule types and optional types
		`type P struct { X *Match, Y []Match, Z Seq }
def main = x:"a"? y:item* z:pair -> new P{X: $x, Y: $y, Z: $z}
def item = @(?a-z)
def pair = "(" ")"`,
		// Optional values as results of rules, actions and map functions
		`def main = x:"a"? -> $x`,
		`def main: *Match = "a"?`,
		`type C struct { X Match, Next *C }
def main: *C = xs:"a"* -> foldr(nil, $xs, (acc, x) => new C{X: $x, Next: $acc})`,
		`def main = xs:p* -> map($xs, (p) => $p.x)
def p = x:@"a"? ";"`,
		// Inference for recursive rules
		`def list = "a" list / _
def main = l:list -> $l`,
		// Predicates and variables
		`def main = [n = 0] s:@" "* [len($s) > n && text($s) != "x"] "a"`,
		// concat over a union of list types (foldl widens []never with []Match) and over a recovered Error
		`def main = xs:item* -> concat(foldl(list(), $xs, (acc, x) => concat($acc, list($x))))
def item = @"a"`,
		`type S struct { X Match }
def main = xs:stmts -> concat($xs)
def stmts = stmt* #recover(skip=(?^;)+ ";")
def stmt: S = x:@"x" ";" -> new S{X: $x}`,
		// Field access and built-in fields
		`type P struct { S int, K []*node }
def main = w:word -> new P{S: $w.startPos + $w.endPos, K: $w.children}
def word = k:"a" v:"b"`,
	} {
		compile(t, src)
	}
}

// TestInferenceOfGrowingTypes checks that inferring a rule type that grows at every round (lists
// of lists of the rule's own values) finishes quickly instead of taking exponential time.
func TestInferenceOfGrowingTypes(t *testing.T) {
	src := `
def main = (r0 / r1) r2?
def r0 = ";"
def r1 = "a"
def r2 = (r3 "é" / "a")
def r3 = (((c1:(r3) [v = len($c1)]) c2:(r1) (";") #recover(skip=(?^;)* ";"))) #error(message="m1") -> map(concat(list($c1), $c1.children), (i) => concat(list($i), map($c1.children, (j) => list($i, $j))))`
	g, err := syntax.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Compile(g, Options{})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("type inference did not finish within 10 seconds")
	}
}

func TestRuleType(t *testing.T) {
	prog := compile(t, `type N terminal
type P struct { X N }
def n: N = @"1"
def p = x:n -> new P{X: $x}
def ps = p*
def opt = "a"?
def main = ps $$`)
	for name, want := range map[string]string{
		"n": "N", "p": "P", "ps": "[]P", "opt": "*Match", "main": "Seq",
	} {
		if got, ok := prog.RuleType(name); !ok || got != want {
			t.Errorf("RuleType(%s) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := prog.RuleType("nope"); ok {
		t.Error("RuleType reported an undefined rule")
	}
	if _, ok := compile(t, `def main = "a"`, Options{NoTypeCheck: true}).RuleType("main"); ok {
		t.Error("RuleType reported a type without type checking")
	}
}
