package engine

import (
	"strings"
	"testing"
	"time"
)

func TestTypeErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`type A = { X Nope }`, `1:14: undefined type Nope`},
		{`type A ~ B
type B ~ A
rule main = "a"`, `type A refers to itself`},
		{`type P = { X node[string] }
type Alias ~ P
rule main = "a" => Alias{X: 1}`, `cannot use int as node[string] in field X of Alias`},
		{`type P = { X node[string] }
type Alias ~ P
rule main = v1:"a" => Alias{Y: $v1}`, `Alias has no field Y`},
		{`type P = {}
type Q = {}
type Alias ~ P | Q
rule main = "a" => Alias{}`, `Alias is not a struct type`},
		{`type P = {}
type Alias ~ *P
rule main = "a" => Alias{}`, `Alias is not a struct type`},
		{`type A ~ B
type B ~ A
rule main = "a" => A{}`, `A is not a struct type`},
		{`type A = { X node[string]; X node[string] }`, `duplicate field X in A`},
		{`rule main int = "a"`, `rule main produces node[string], which is not assignable to int`},
		{`type P = { X node[string] }
rule main = "a" => P{X: 1}`, `cannot use int as node[string] in field X of P`},
		{`type Num = { Text string }
type P = { N Num }
rule main = n:"1" #atomic => P{N: $n}`, `cannot use node[string] as Num in field N of P`},
		{`type P = { X node[string] }
rule main = x:"a"? => P{X: $x}`, `cannot use *node[string] as node[string] in field X of P`},
		{`type P = { X node[string] }
rule main = x:"a" / "b" => P{X: $x}`, `cannot use *node[string] as node[string] in field X of P`},
		{`rule main = x:("a" y:"b")? => $x.$y`, `cannot access .$y of *node[(node[string], node[string]), {"y": 1}]: the value may be nil`},
		{`rule main = xs:(k:"a")* => $xs.$k`, `node[[]node[(node[string],), {"k": 0}]] has no capture k`},
		{`rule main = "a" => len(1 + "a")`, `invalid operands int and string for +`},
		{`rule main = "a" => len(1)`, `len: invalid argument int`},
		{`var x int
rule main = "a" {x := (y) => 1}`, `functions can only be passed to filter, foldl, foldr, or map`},
		{`type Op = { X node[string] }
rule main Op = "a"`, `rule main produces node[string], which is not assignable to Op`},
		{`type E = { X node[string] }
rule e E = operators {
    operand v1:"1" => E{X: $v1}
    level { infix left "+" }
}`, `operators of e without an action produce a tuple node, which is not assignable to E`},
		{`rule main = v1:"a" => foldl($v1, $v1, (a, b) => a)`, `foldl: expected a list, got node[string]`},
		{`rule main = "a" => list((x) => x)`, `functions can only be passed to filter, foldl, foldr, or map`},
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
		// Rules, actions and list elements may produce ints, strings and bools.
		`rule main int = d:[0-9]+ #atomic => len($d)`,
		`rule main = "a" => 1`,
		`rule main = xs:"a"* => map($xs, (x) => len(x))`,
		`rule main = "a" => list(1, "s", true, nil)`,
		// Join types and the type of the foldl accumulator
		`type Number = { Text string }
type Op = { Left Node; Op node[string]; Right Node }
type Node ~ Op | Number
rule expr Node = l:num rest:(op:("+" / "-") #atomic r:num)* => foldl($l, $rest, (acc, i) => Op{Left: acc, Op: i.$op, Right: i.$r})
rule num Number = t:([0-9]+) #atomic => Number{Text: text($t)}`,
		// The accumulator type is widened by inference
		`type Op = { L node[string] | Op; R node[string] }
rule expr = l:"x" #atomic rest:(r:"y" #atomic)* => foldl($l, $rest, (acc, i) => Op{L: acc, R: i.$r})
rule main = e:expr => $e`,
		// Inferred rule types and optional types
		`type P = { X *node[string]; Y []node[string]; Z node[_] }
rule main = x:"a"? y:item* z:pair => P{X: $x, Y: $y.Value, Z: $z}
rule item = [a-z] #atomic
rule pair = "(" ")"`,
		// Optional values as results of rules, actions and map functions
		`rule main = x:"a"? => $x`,
		`rule main *node[string] = "a"?`,
		`type C = { X node[string]; Next *C }
rule main *C = xs:"a"* => foldr(nil, $xs, (acc, x) => C{X: x, Next: acc})`,
		`rule main = xs:p* => map($xs, (p) => p.$x)
rule p = x:"a"? #atomic ";"`,
		// Inference for recursive rules
		`rule list = "a" list / _
rule main = l:list => $l`,
		// Predicates and variables
		`var n int
rule main = {n := 0} s:" "* #atomic {len($s) > n && text($s) != "x"} "a"`,
		// concat over a union of list types (foldl widens []never with []node[string]) and over a recovered error
		`rule main = xs:item* => concat(foldl(list(), $xs, (acc, x) => concat(acc, list(x))))
rule item = "a" #atomic`,
		`type S = { X node[string] }
rule main = xs:stmts => $xs
rule stmts = stmt* #recover(skip=[^;]+ ";")
rule stmt S = x:"x" #atomic ";" => S{X: $x}`,
		// Field access and built-in fields
		`type P = { S int; K _ }
rule main = w:word => P{S: $w.Start + $w.End, K: $w.Value}
rule word = k:"a" v:"b"`,
	} {
		compile(t, src)
	}
}

// TestInferenceOfGrowingTypes checks that inferring a rule type that grows at every round (lists
// of lists of the rule's own values) finishes quickly instead of taking exponential time.
func TestInferenceOfGrowingTypes(t *testing.T) {
	src := `var v int
rule main = (r0 / r1) r2?
rule r0 = ";"
rule r1 = "a"
rule r2 = r3 "é" / "a"
rule r3 = ((c1:r3 {v := len($c1)}) c2:r1 ";" #recover(skip=[^;]* ";")) @error(message="m1") => map(concat(list($c1), $c1), (i) => concat(list(i), map($c1, (j) => list(i, j))))`
	g, err := parseTest(src)
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
	// A reference to a rule with a declared type shows that type; one to a rule without a declared
	// type shows the rule's name.
	prog := compile(t, `type N = { Text string }
type P = { X N }
rule n N = t:("1" #atomic) => N{Text: text($t)}
rule nn = n
rule p = x:n => P{X: $x}
rule ps = p*
rule pl = xs:p* => $xs.Value
rule opt = "a"?
rule on *N = n?
rule oo = on?
rule any node[_] = "a" "b"
rule main = ps any $$`)
	for name, want := range map[string]string{
		"n": "N", "nn": "N", "p": "P", "ps": "node[[]p]", "pl": "[]p", "opt": "*node[string]", "on": "*N", "oo": "*N",
		"any": "node[_]", "main": "node[(ps, node[_])]",
	} {
		if got, ok := prog.RuleType(name); !ok || got != want {
			t.Errorf("RuleType(%s) = %q, %v; want %q", name, got, ok, want)
		}
	}
	if _, ok := prog.RuleType("nope"); ok {
		t.Error("RuleType reported an undefined rule")
	}
	data, err := compile(t, `rule main = "a"`).MarshalBinary("main")
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := LoadProgram(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.RuleType("main"); ok {
		t.Error("RuleType reported a type for a program loaded from a compiled grammar")
	}
}

// TestAliasErrorsReportedOnce checks that an error inside an alias is reported once, however many
// places resolve the alias.
func TestAliasErrorsReportedOnce(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"type A ~ Nope\nrule main A = \"a\"", "1:10: undefined type Nope"},
		{"type A ~ Nope\ntype B = { F A; G []A }\nrule main A = \"a\"\nrule other A = \"b\"\nrule x = v:main => B{F: $v, G: list($v)}", "1:10: undefined type Nope"},
		{"type A ~ *A\nrule main A = \"a\"\nrule other A = \"b\"", "type A refers to itself"},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		_, err = Compile(g, Options{})
		if err == nil || strings.Count(err.Error(), tc.want) != 1 {
			t.Errorf("%q: got %v, want %q once", tc.src, err, tc.want)
		}
	}
}
