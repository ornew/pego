package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The value types of rules follow the value model (spec/language/types.md).
func TestValueTypes(t *testing.T) {
	for _, tc := range []struct{ src, rule, want string }{
		// The examples of the record.
		{`rule a = "x"`, "a", `node[string]`},
		{`rule a = ("x" "y"*) #atomic`, "a", `node[string]`},
		{"rule a = b c d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, c, d)]`},
		{"rule a = b+\nrule b = \"b\"", "a", `node[[]b]`},
		{`rule a = "a"?`, "a", `*node[string]`},
		{"rule a = b? c\nrule b = \"b\"\nrule c = \"c\"", "a", `node[(*b, c)]`},
		{"type Foo = {}\nrule a = _ => Foo{}\nrule b = a \"a\"*", "a", `Foo`},
		{"type Foo = {}\nrule a = _ => Foo{}\nrule b = a \"a\"*", "b", `node[(a, node[[]node[string]])]`},
		// Call-only rules are transparent.
		{"rule a = b\nrule b = \"b\"", "a", `b`},
		// Sequences are always tuples, and groups flatten.
		{"rule a = -ws b $$\nrule b = \"b\"\nrule ws = \" \"*", "a", `node[(b,)]`},
		{"rule a = b (c d)\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, c, d)]`},
		{"rule a = (b c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, c, d)]`},
		// Valueless bodies produce the unit node.
		{`rule stub = _`, "stub", `node[()]`},
		{`rule a = &"a" !"b"`, "a", `node[()]`},
		// Captures index the tuple; a capture in a choice shares its element.
		{"rule a = x:b \"=\" y:c\nrule b = \"b\"\nrule c = \"c\"", "a", `node[(b, node[string], c), {"x": 0, "y": 2}]`},
		{"rule a = (x:b / y:c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b | c, d), {"x": 0, "y": 0}]`},
		{`rule a = x:"x"`, "a", `node[(node[string],), {"x": 0}]`},
		// A repetition element with captures is a node of its tuple.
		{`rule a = x:(y:.)*`, "a", `node[(node[[]node[(node[string],), {"y": 0}]],), {"x": 0}]`},
		// A captured sequence is one element; captures inside it have longer paths.
		{"rule a = x:(y:b c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(node[(b, c), {"y": 0}], d), {"x": 0, "y": 0.0}]`},
		// An optional sequence is an optional node of its tuple.
		{"rule a = (b c)? d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(*node[(b, c)], d)]`},
		// A choice with a valueless alternative is optional.
		{`rule a = "a" / &"b"`, "a", `*node[string]`},
		// Recovery is an explicit union; what recovers is one element.
		{`rule a = "a" #missing`, "a", `node[string] | missing`},
		{"rule a = b (c d) #recover(skip=[^;]*) \";\"\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(b, node[(c, d)] | error, node[string])]`},
		// A capture in alternatives at the same position has that position.
		{"rule a = (y:b / y:d) e\nrule b = \"b\"\nrule d = \"d\"\nrule e = \"e\"", "a", `node[(b | d, e), {"y": 0}]`},
		// A capture in alternatives of different shapes has a path for each.
		{"rule a = (y:b c / y:d) e\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"\nrule e = \"e\"", "a", `node[(node[(b, c), {"y": 0}] | d, e), {"y": 0.0 | 0}]`},
		// A capture in a lookahead in one alternative is kept beside the tuple when that one matched.
		{"rule a = (x:b / &(-c x:d)) e\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"\nrule e = \"e\"", "a", `node[(*b, e), {"x": 0 | -}]`},
		// A capture in a positive lookahead has no position; its value is kept beside the tuple.
		{"rule a = &(y:b) c\nrule b = \"b\"\nrule c = \"c\"", "a", `node[(c,), {"y": -}]`},
		{"rule a = &(y:b c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(d,), {"y": -}]`},
		{"rule a = x:(&(y:b) c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(node[(c,), {"y": -}], d), {"x": 0, "y": -}]`},
		{"rule a = (&(y:b) / c) d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `node[(*c, d), {"y": -}]`},
		// T <= *T, and a valueless alternative makes the choice optional.
		{`rule a = ("a"? / "b") "c"`, "a", `node[(*node[string], node[string])]`},
		{`rule a = ("a" / -"b" -"c") "d"`, "a", `node[(*node[string], node[string])]`},
		{`rule a = (("a")?)? "d"`, "a", `node[(*node[string], node[string])]`},
		// Operator lines produce tuple nodes.
		{"rule e = #operators {\n    operand n\n    level { infix left \"+\" }\n    level { prefix \"-\" }\n}\nrule n = [0-9]+", "e",
			`n | node[(e, node[string], e), {"lhs": 0, "op": 1, "rhs": 2}] | node[(node[string], e), {"op": 0, "rhs": 1}]`},
		// A rule with an action has the action's type, in which a capture has the type of its value.
		{`rule a = x:"1"* => $x`, "a", `node[[]node[string]]`},
	} {
		g, err := parseTest(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		p, err := Compile(g, Options{})
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if got := p.valueTypes()[tc.rule].String(); got != tc.want {
			t.Errorf("%q: rule %s\ngot  %s\nwant %s", tc.src, tc.rule, got, tc.want)
		}
	}
}

// Every rule of the repository grammars has a value type.
func TestValueTypesOfRepositoryGrammars(t *testing.T) {
	var paths []string
	for _, pattern := range []string{"../../examples/*/*.pego", "../../parsers/*/*.pego", "../../spec/*/*.pego"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, m...)
	}
	if len(paths) < 10 {
		t.Fatalf("found only %d grammars", len(paths))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		g, err := parseTest(string(data))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		p, err := Compile(g, Options{})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		p.valueErrors() // checks every action without failing
		for _, rd := range g.Rules() {
			if v, ok := p.valueTypes()[rd.Name]; !ok || v == nil || v == tyBottom {
				t.Errorf("%s: rule %s has value type %v", path, rd.Name, v)
			}
		}
	}
}

// A capture under ? is optional in the enclosing node only, not in the node it captures.
func TestValueTypesOptionalCaptures(t *testing.T) {
	g, err := parseTest("rule a = (x:b c)? d\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	outer := p.valueTypes()["a"].(nodeTy)
	if len(outer.caps) != 1 || outer.caps[0].t.String() != "*b" {
		t.Fatalf("outer captures %+v", outer.caps)
	}
	inner := outer.value.(tupleTy).elems[0].(optTy).elem.(nodeTy)
	if len(inner.caps) != 1 || inner.caps[0].t.String() != "b" {
		t.Fatalf("inner captures %+v", inner.caps)
	}
}

// valueTypesOf builds src without running the type check, as a compiled grammar is rebuilt, and returns the value types of its rules and
// the errors of its actions. It accepts earlier syntax, which still reaches the engine from JSON
// grammars.
func valueTypesOf(t *testing.T, src string) (map[string]ty, ErrorList) {
	t.Helper()
	g, err := parseLegacyTest(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	p, err := build(g, Options{}, make([]ruleFlags, len(g.Rules())))
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return valueTypes(p.staticProgram, nil)
}

// Actions are typed in the value model: captures have the types of their values, and nodes expose
// .Start, .End, .Value and their captures.
func TestValueActionTypes(t *testing.T) {
	for _, tc := range []struct{ src, rule, want string }{
		{`rule a = x:"x" => $x.Value`, "a", `string`},
		{`rule a = x:"x" => $x.Start`, "a", `int`},
		{`rule a = x:"x" => $x.End - $x.Start`, "a", `int`},
		{`rule a = _ => 100`, "a", `int`},
		{`rule a = x:"x"* => $x.Value`, "a", `[]node[string]`},
		{`rule a = x:"x"* => len($x)`, "a", `int`},
		{`rule a = x:"x"* => len($x.Value)`, "a", `int`},
		// text is the input a node spans, whatever its value.
		{`rule a = x:("x" "y")* => text($x)`, "a", `string`},
		{`rule a = x:"x"? => text($x)`, "a", `string`},
		{"rule a = x:(b / c)? => text($x)\nrule b = \"b\" \"1\"\nrule c = \"c\"", "a", `string`},
		{"rule a = x:(b / c)? => len($x)\nrule b = \"b\" \"1\"\nrule c = \"c\"", "a", `int`},
		// The example of the record: map over the node of a repetition reads its elements.
		{`rule a = x:(y:.)* => map($x, (i) => i.$y)`, "a", `[]node[string]`},
		{`rule a = x:(y:.)* => map($x.Value, (i) => i.Value[0])`, "a", `[]node[string]`},
		// Captures of rules have the rules' value types.
		{"rule a = x:b => $x.Value\nrule b = \"b\"", "a", `string`},
		{"rule a = x:b => $x.$y\nrule b = y:\"b\" \"c\"", "a", `node[string]`},
		{"type Foo = { Name string }\nrule a = x:b => $x.Name\nrule b = y:\"b\" => Foo{Name: $y.Value}", "a", `string`},
		// A capture that may not match is optional; one in every alternative is not.
		{`rule a = (x:"a" / "b") => $x`, "a", `*node[string]`},
		{`rule a = (x:"a" / x:"b") => $x.Value`, "a", `string`},
		{`rule a = x:"a"? => $x`, "a", `*node[string]`},
		// .$y on a union needs every argument to capture y, and joins their capture types.
		{"rule a = x:(b / c) => $x.$y\nrule b = y:\"b\" \"1\"\nrule c = y:d \"2\"\nrule d = \"d\"", "a", `node[string] | d`},
		// A member that only some arguments of a union have is optional.
		{`rule a = x:"a" #missing => $x.Value`, "a", `*string`},
		{`rule a = x:"a" #missing => $x.Start`, "a", `int`},
		// Indexing a tuple with a constant reads that element.
		{"rule a = x:(b c) => $x.Value[1]\nrule b = \"b\"\nrule c = \"c\"", "a", `c`},
		{"rule a = x:(b c) => $x.Value[0]\nrule b = \"b\"\nrule c = \"c\"", "a", `b`},
		// Indexing a node indexes its Value.
		{"rule a = x:(b c) => $x[1]\nrule b = \"b\"\nrule c = \"c\"", "a", `c`},
		{`rule a = x:(y:.)* => map($x.Value, (i) => i[0])`, "a", `[]node[string]`},
		// List functions.
		{`rule a = x:"x"* => filter($x, (i) => len(i) > 0)`, "a", `[]node[string]`},
		{`rule a = x:"x"* => foldl(0, $x, (n, i) => n + len(i))`, "a", `int`},
		{`rule a = x:"x" y:"y"* => concat(list($x), $y)`, "a", `[]node[string]`},
		{`rule a = x:"x" y:"1" => list($x.Value, $y.Start)`, "a", `[](string | int)`},
		{`rule a = x:"x" => list($x, nil)`, "a", `[]*node[string]`},
		// Struct fields and constructors use the declared types, with Match a string node.
		{"type Foo = { Name node[string] }\nrule a = x:\"x\" => Foo{Name: $x}", "a", `Foo`},
		{"type Foo = { Items []node[string] }\nrule a = x:\"x\"* => Foo{Items: $x.Value}", "a", `Foo`},
		// Operators Rule actions see $lhs, $op and $rhs.
		{"rule e = #operators {\n    operand n:[0-9]+ => $n.Value\n    level { infix left \"+\" => $op }\n}", "e", `[]node[string] | node[string]`},
		// A declared type is the rule's type, node[_] included.
		{"type Foo = {}\nrule a Foo = _ => Foo{}", "a", `Foo`},
		{`rule a node[_] = x:"x" => $x`, "a", `node[_]`},
		{`rule a node[_] = "x" "y"`, "a", `node[_]`},
		{"type Word = { Text string }\nrule a Word = t:([a-z]+) #atomic => Word{Text: text($t)}", "a", `Word`},
		// A rule whose action depends on its own value sees it as not known.
		{"rule a = \"(\" x:b \")\" => $x.Value\nrule b = a / \"y\"", "a", `unknown`},
		// _|_ | T is T, so a _|_ alternative does not make a choice optional.
		{`rule a = x:("a" / _|_) => $x.Value`, "a", `string`},
		{`rule a = x:("a" / _|_ @error(message="m")) => $x.Value`, "a", `string`},
		{`rule a = x:("a" / _|_ #recover(skip="x")) => $x`, "a", `node[string] | error`},
		{`rule a = ("a" / _|_) "b"`, "a", `node[(node[string], node[string])]`},
		// nil joins as an optional type, and a value not known yet absorbs a union.
		{`rule a = _ => list(nil)`, "a", `[]nil`},
		{`rule a = x:"x" => list($x, nil, $x)`, "a", `[]*node[string]`},
		{`var n int
rule a = x:"x" {n := 1} => list($x, n)`, "a", `[](node[string] | int)`},
		// An action reads a capture in a positive lookahead.
		{`rule a = &(y:"y") x:"x" => $y`, "a", `node[string]`},
		{"rule a = (x:b / &(-c x:d)) => $x\nrule b = \"b\"\nrule c = \"c\"\nrule d = \"d\"", "a", `b | d`},
		// The rules of a cycle through actions are the least fixed point of their values.
		{"rule a = x:\"x\" y:b? => concat(list($x), $y)\nrule b = z:a => $z", "a", `[]node[string]`},
		// A rule type is not expanded unless an action reads into it.
		{"rule a = x:\"x\" y:b? => list($x, $y)\nrule b = z:a => $z", "a", `[]*(node[string] | b)`},
		// Rule references stay symbolic, so a recursive rule needs no expansion.
		{`rule a = x:("(" a ")")? => $x`, "a", `*node[(node[string], a, node[string])]`},
	} {
		types, errs := valueTypesOf(t, tc.src)
		if len(errs) > 0 {
			t.Errorf("%q: %v", tc.src, errs)
		}
		if got := types[tc.rule].String(); got != tc.want {
			t.Errorf("%q: rule %s\ngot  %s\nwant %s", tc.src, tc.rule, got, tc.want)
		}
	}
}

// Actions with type errors are reported in the value model.
func TestValueActionErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`rule a = x:"x" => $x.Source`, "the source information of a node is not accessible from actions"},
		{`rule a = x:"x" => text(1)`, "text: invalid argument int"},
		{`rule a = x:"x" => text(list($x))`, "text: invalid argument []node[string]"},
		{`rule a = x:"x" => $x.startPos`, ".startPos is removed: use .Start or .End"},
		{`rule a = x:"x" => $x.children`, ".children is removed: use .Value"},
		{`rule a = x:(y:"y" "z") => $x.y`, `node[(node[string], node[string]), {"y": 0}] has no field y: read the capture with .$y`},
		{`rule a = x:"x" => $x.$y`, "node[string] has no capture y"},
		{"rule a = x:(b / c) => $x.$y\nrule b = y:\"b\" \"1\"\nrule c = \"c\"", "node[string] has no capture y"},
		{`rule a = x:"x"? => $x.Value`, "cannot access .Value of *node[string]: the value may be nil"},
		{`rule a = x:"x" => $x.Value + 1`, "invalid operands string and int for +"},
		{`rule a = x:"x" => $x[0]`, "cannot index node[string]"},
		{"rule a = x:(b c) => $x[2]\nrule b = \"b\"\nrule c = \"c\"", "index 2 is out of range for (b, c)"},
		{"rule a = x:(b c) => $x.Value[2]\nrule b = \"b\"\nrule c = \"c\"", "index 2 is out of range for (b, c)"},
		{`rule a = x:"x" => map($x, (i) => i)`, "map: expected a list, got node[string]"},
		{"type Foo = { Name string }\nrule a = x:\"x\" => Foo{Name: $x}", "cannot use node[string] as string in field Name of Foo"},
		{"type Foo = { Name node[string] }\nrule a = x:\"x\" #missing => Foo{Name: $x}", "cannot use node[string] | missing as node[string] in field Name of Foo"},
		{"type Foo = {}\nrule a = x:\"x\" => []Foo{$x}", "cannot use node[string] as Foo in list constructor"},
		// The retired forms are read as the text structs they mean: a terminal rule without an
		// action does not produce one, and a positional constructor sets the field Text.
		{"type Word terminal\nrule a Word = [a-z]+", "rule a produces node[[]node[string]], which is not assignable to Word"},
		{"type Word terminal\nrule a = _ => Word{1}", `cannot use int as string in field Text of Word`},
		{"type Word = { Text string }\nrule a = _ => Word{Text: 1}", `cannot use int as string in field Text of Word`},
		{"type Foo = {}\nrule a Foo = x:\"x\" => $x", "rule a produces node[string], which is not assignable to Foo"},
		{`rule a = x:"x" => filter($x.Value, (i) => i)`, "filter: expected a list, got string"},
		{"rule a = x:\"x\" y:b => list($x, $y.Value)\nrule b = z:a => $z", "[]node[string] | []unknown has no field Value"},
	} {
		_, errs := valueTypesOf(t, tc.src)
		found := false
		for _, e := range errs {
			found = found || e.Msg == tc.want
		}
		if !found {
			t.Errorf("%q: want error %q, got %v", tc.src, tc.want, errs)
		}
	}
}

// The rules of a cycle of four rules through actions have the same types and errors in every
// order.
func TestValueActionCycleOrder(t *testing.T) {
	rules := []string{
		"rule r = x:a y:c \"q\" => len($x) + len($y)",
		"rule a = x:b z:r \"q\" => len($x) + len($z)",
		"rule b = x:a \"q\" => $x + 1",
		"rule c = x:b \"q\" => $x + 1",
	}
	var want string
	var permute func(k int)
	permute = func(k int) {
		if k == len(rules) {
			types, errs := valueTypesOf(t, strings.Join(rules, "\n"))
			if len(errs) == 0 {
				t.Fatal("no errors: len of an int is an error")
			}
			var msgs []string
			for _, e := range errs {
				msgs = append(msgs, e.Msg)
			}
			got := fmt.Sprintf("r=%s a=%s b=%s c=%s %v", types["r"], types["a"], types["b"], types["c"], msgs)
			if want == "" {
				want = got
			} else if got != want {
				t.Errorf("order %q:\ngot  %s\nwant %s", rules, got, want)
			}
			return
		}
		for i := k; i < len(rules); i++ {
			rules[k], rules[i] = rules[i], rules[k]
			permute(k + 1)
			rules[k], rules[i] = rules[i], rules[k]
		}
	}
	permute(0)
}

// A cycle whose values grow in every round ends with values not known.
func TestValueActionCycleWidens(t *testing.T) {
	types, errs := valueTypesOf(t, `rule a = x:"x" y:a? => list(concat(list($x), $y))`)
	if len(errs) > 0 {
		t.Errorf("errors %v", errs)
	}
	if got := types["a"].String(); !strings.Contains(got, "unknown") {
		t.Errorf("got %s", got)
	}
}

// Errors in rules computed on demand are kept, also from a lambda typed more than once, and the
// rules of a cycle through actions have the same types and errors in either order.
func TestValueActionsOnDemand(t *testing.T) {
	for _, src := range []string{
		"rule a = x:\"x\"* y:b => foldl(0, $x, (n, i) => n + len($y.Value))\nrule b = z:\"z\" => $z.Source",
		"rule b = z:\"z\" => $z.Source\nrule a = x:\"x\"* y:b => foldl(0, $x, (n, i) => n + len($y.Value))",
	} {
		if _, errs := valueTypesOf(t, src); len(errs) != 1 {
			t.Errorf("%q: errors %v", src, errs)
		}
	}
	var want string
	for _, src := range []string{
		"rule a = x:b => len($x)\nrule b = y:a => $y + \"s\"",
		"rule b = y:a => $y + \"s\"\nrule a = x:b => len($x)",
	} {
		types, errs := valueTypesOf(t, src)
		var msgs []string
		for _, e := range errs {
			msgs = append(msgs, e.Msg)
		}
		got := fmt.Sprintf("a=%s b=%s %v", types["a"], types["b"], msgs)
		if want == "" {
			want = got
		} else if got != want {
			t.Errorf("order changes the result:\n%s\n%s", want, got)
		}
		if !strings.Contains(got, "invalid operands int and string for +") {
			t.Errorf("%q: %s", src, got)
		}
	}
}

// The value checker reports the errors of actions that do not depend on values being nodes.
func TestValueCheckerDiagnostics(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`var x int
rule main = "a" {x := (y) => 1}`, "functions can only be passed to filter, foldl, foldr, or map"},
		{`rule main = xs:"a"* => map($xs, (x, y) => x)`, "map: expected a function of 1 parameters"},
		{`rule main = xs:"a"* => filter($xs, 1)`, "filter: expected a function of 1 parameters"},
		{`rule main = xs:"a"* => foldl(nil, $xs, (a) => a)`, "foldl: expected a function of 2 parameters"},
		{"var v string\nrule main = x:\"a\" {v := $x} \"b\"", "cannot use node[string] as string in the binding of variable v"},
		{"var v int\nrule main = \"a\" {v := \"s\"} \"b\"", "cannot use string as int in the binding of variable v"},
		{`rule main = x:"a" => $x.(int)`, "cannot cast node[string] to int"},
		{`rule main = "a" => (1).(string)`, "cannot cast int to string"},
		{"rule main = x:n => $x.(P)\n" + castRules, "cannot cast Num to P"},
		{"type Int = { Text string }\nvar x int\nrule main = {x := 1} \"x\" => []Int{x}", "cannot use int as Int in list constructor"},
		{"type Int = { Text string }\nrule main = d:dyn => []Int{$d}\nrule dyn _ = ds / di\nrule ds = \"b\" => \"s\"\nrule di = _ => 1", "cannot use _ as Int in list constructor: the type of the value is not known; convert it with a cast, as in value.(Int)"},
		{"type E = { X node[string] }\nrule e E = #operators {\n    operand v1:\"1\" => E{X: $v1}\n    level { infix left \"+\" }\n}",
			"operators of e without an action produce a tuple node, which is not assignable to E"},
		{"type E = { X node[string] }\nrule e E = #operators {\n    operand \"1\"\n}", "operatorsRule e produces node[string], which is not assignable to E"},
	} {
		_, errs := valueTypesOf(t, tc.src)
		found := false
		for _, e := range errs {
			found = found || e.Msg == tc.want
		}
		if !found {
			t.Errorf("%q: want error %q, got %v", tc.src, tc.want, errs)
		}
	}
	// Casts that can succeed, variables of the allowed types and functions passed to list
	// functions are not errors.
	for _, src := range []string{
		"rule main = x:(n / w) $$ => $x.(Num)\n" + castRules,
		"rule main = x:(n+ / w) $$ => $x.([]Num)\n" + castRules,
		"rule main = x:(n / w)? $$ => $x.(*Num)\n" + castRules,
		"rule main = x:(n / w / pair) $$ => $x.(Num | P)\n" + castRules,
		"rule main = x:(pair / w) $$ => $x.(P).N\n" + castRules,
		`var v string
var n int
var b bool
rule main = x:"a" {v := text($x)} {n := len($x)} {b := n > 0} "b"`,
		`rule main = xs:"a"* => foldl(0, $xs, (a, x) => a + 1)`,
		// A list whose elements may be nil holds none when the cast succeeds.
		`rule main = xs:("a"?)* => $xs.Value.([]node[string])`,
		"type Int = { Text string }\nrule main = x:i => []Int{$x}\nrule i Int = t:\"1\" => Int{Text: text($t)}",
	} {
		if _, errs := valueTypesOf(t, src); len(errs) > 0 {
			t.Errorf("%q: unexpected errors %v", src, errs)
		}
	}
}
