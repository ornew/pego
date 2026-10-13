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
rule main = "a"`, `type A refers to itself`},
		{`type P struct { X Match }
type Alias = P
rule main = "a" => Alias{X: 1}`, `cannot use int as Match in field X of Alias`},
		{`type P struct { X Match }
type Alias = P
rule main = "a" => Alias{Y: $1}`, `Alias has no field Y`},
		{`type P struct {}
type Q struct {}
type Alias = P | Q
rule main = "a" => Alias{}`, `Alias is not a struct type`},
		{`type P struct {}
type Alias = *P
rule main = "a" => Alias{}`, `Alias is not a struct type`},
		{`type A = B
type B = A
rule main = "a" => A{}`, `A is not a struct type`},
		{`type A struct { X Match; X Match }`, `duplicate field X in A`},
		{`rule main int = "a"`, `rule main must produce a node, but its type is int`},
		{`type P struct { X Match }
rule main = "a" => P{X: 1}`, `cannot use int as Match in field X of P`},
		{`type Num terminal
type P struct { N Num }
rule main = n:"1" #atomic => P{N: $n}`, `cannot use Match as Num in field N of P`},
		{`type P struct { X Match }
rule main = x:"a"? => P{X: $x}`, `cannot use *Match as Match in field X of P`},
		{`type P struct { X Match }
rule main = x:"a" / "b" => P{X: $x}`, `cannot use *Match as Match in field X of P`},
		{`rule main = x:("a" y:"b")? => $x.y`, `cannot access .y of *Seq: the value may be nil`},
		{`rule main = xs:(k:"a")* => $xs.k`, `[]Seq{k: Match} has no field k`},
		{`rule main = "a" "b" => $3`, `$3 is out of range: the rule has 2 elements with a value`},
		{`rule main = "a" => len(1 + "a")`, `invalid operands int and string for +`},
		{`rule main = "a" => len(1)`, `len: invalid argument int`},
		{`rule main = "a" => 1`, `action of main must produce a node, got int`},
		{`rule main = {x := $1} "a"
rule b = "b"`, `1:19: $1 cannot be used in predicates`},
		{`rule main = "a" {x := (y) => 1}`, `functions can only be passed to foldl, foldr, or map`},
		{`type Op struct { X Match }
rule main Op = "a"`, `rule main produces Match, which is not assignable to Op`},
		{`type E struct { X Match }
rule e E = operators {
    operand "1" => E{X: $1}
    level { infix left "+" }
}`, `operators of e without an action produce Operator, which is not assignable to E`},
		{`rule main = "a" => foldl($1, $1, (a, b) => $a)`, `foldl: expected a list, got Match`},
		{`rule main = xs:"a"* => map($xs, (x) => len($x))`, `map: the function must produce a node, got int`},
		{`rule main = "a" => list(1)`, `list: elements must be nodes, got int`},
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
type Op struct { Left Node; Op Match; Right Node }
type Node = Op | Number
rule expr Node = l:num rest:(op:("+" / "-") #atomic r:num)* => foldl($l, $rest, (acc, i) => Op{Left: $acc, Op: $i.op, Right: $i.r})
rule num Number = [0-9]+`,
		// The accumulator type is widened by inference
		`type Op struct { L node; R Match }
rule expr = l:"x" #atomic rest:(r:"y" #atomic)* => foldl($l, $rest, (acc, i) => Op{L: $acc, R: $i.r})
rule main = e:expr => $e`,
		// Inferred rule types and optional types
		`type P struct { X *Match; Y []Match; Z Seq }
rule main = x:"a"? y:item* z:pair => P{X: $x, Y: $y, Z: $z}
rule item = [a-z] #atomic
rule pair = "(" ")"`,
		// Optional values as results of rules, actions and map functions
		`rule main = x:"a"? => $x`,
		`rule main *Match = "a"?`,
		`type C struct { X Match; Next *C }
rule main *C = xs:"a"* => foldr(nil, $xs, (acc, x) => C{X: $x, Next: $acc})`,
		`rule main = xs:p* => map($xs, (p) => $p.x)
rule p = x:"a"? #atomic ";"`,
		// Inference for recursive rules
		`rule list = "a" list / _
rule main = l:list => $l`,
		// Predicates and variables
		`rule main = {n := 0} s:" "* #atomic {len($s) > n && text($s) != "x"} "a"`,
		// concat over a union of list types (foldl widens []never with []Match) and over a recovered Error
		`rule main = xs:item* => concat(foldl(list(), $xs, (acc, x) => concat($acc, list($x))))
rule item = "a" #atomic`,
		`type S struct { X Match }
rule main = xs:stmts => concat($xs)
rule stmts = stmt* #recover(skip=[^;]+ ";")
rule stmt S = x:"x" #atomic ";" => S{X: $x}`,
		// Field access and built-in fields
		`type P struct { S int; K []*node }
rule main = w:word => P{S: $w.startPos + $w.endPos, K: $w.children}
rule word = k:"a" v:"b"`,
	} {
		compile(t, src)
	}
}

// TestInferenceOfGrowingTypes checks that inferring a rule type that grows at every round (lists
// of lists of the rule's own values) finishes quickly instead of taking exponential time.
func TestInferenceOfGrowingTypes(t *testing.T) {
	src := `rule main = (r0 / r1) r2?
rule r0 = ";"
rule r1 = "a"
rule r2 = r3 "é" / "a"
rule r3 = ((c1:r3 {v := len($c1)}) c2:r1 ";" #recover(skip=[^;]* ";")) @error(message="m1") => map(concat(list($c1), $c1.children), (i) => concat(list($i), map($c1.children, (j) => list($i, $j))))`
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
rule n N = "1" #atomic
rule p = x:n => P{X: $x}
rule ps = p*
rule opt = "a"?
rule main = ps $$`)
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
	if _, ok := compile(t, `rule main = "a"`, Options{NoTypeCheck: true}).RuleType("main"); ok {
		t.Error("RuleType reported a type without type checking")
	}
}
