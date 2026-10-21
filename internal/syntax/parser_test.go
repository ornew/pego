package syntax

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestParseExpr(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"a"`, `"a"`},
		{`"a\n\t\"\\\u{1F600}"`, `"a\n\t\"\\😀"`},
		{`a b c`, `a b c`},
		{`a / b c / d`, `a / b c / d`},
		{`(a / b) c`, `(a / b) c`},
		{`a* b+ c? d #repeat(2) e #repeat(2,) f #repeat(2, 3) g #repeat(0, 3)`, `a* b+ c? d #repeat(2) e #repeat(2,) f #repeat(2, 3) g #repeat(,3)`},
		{`&a !b c #atomic -d`, `&a !b c #atomic -d`},
		{`x:a y:(b c)* z:d #atomic`, `x:a y:(b c)* z:d #atomic`},
		{`!"if" [a-z]+`, `!"if" [a-z]+`},
		{`[0-9a-zA-Z_]`, `[0-9a-zA-Z_]`},
		{`[\-(?)]`, `[\-(?)]`},
		{`[^"\\]`, `[^"\\]`},
		{`_ _|_ #cut ^^ ^ $ $$ .`, `_ _|_ #cut ^^ ^ $ $$ .`},
		{`level(expr, assignment) expr (a)`, `level(expr, assignment) expr (a)`},
		{`{len($s) > indent} {indent := len($s)}`, `{len($s) > indent} {indent := len($s)}`},
		{`{a + b * c == 1 && !x || y}`, `{a + b * c == 1 && !x || y}`},
		{`{(a + b) * -c}`, `{(a + b) * -c}`},
		{`"hello" / _|_ @error(message="expect 'hello'")`, `"hello" / _|_ @error(message="expect 'hello'")`},
		{`a* #stream @x`, `a* #stream @x`},
		{"(a\n  // comment\n  b)", "(\n    a\n    // comment\n    b\n)"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			e, err := ParseExpr(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := grammar.FormatExpr(e); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestParseExprStructure(t *testing.T) {
	e, err := ParseExpr(`l:term rest:(op:"+" r:term)*`)
	if err != nil {
		t.Fatal(err)
	}
	seq := e.(*grammar.Seq)
	if c := seq.Items[0].(*grammar.Capture); c.Name != "l" {
		t.Errorf("first capture %s", c.Name)
	}
	rest := seq.Items[1].(*grammar.Capture)
	rep := rest.Expr.(*grammar.Repeat)
	if rep.Min != 0 || rep.Max != -1 {
		t.Errorf("repeat %d,%d", rep.Min, rep.Max)
	}
	group, ok := rep.Expr.(*grammar.Group)
	if !ok {
		t.Fatalf("repeat body %T", rep.Expr)
	}
	if _, ok := group.Expr.(*grammar.Seq); !ok {
		t.Errorf("group body %T", group.Expr)
	}
}

const calc = `package calc

// --- Type definitions ---
type Number = { Text string }
type OpType = { Text string }

type Op = {
    Left  Node
    Op    OpType
    Right Node
}

type Node ~ Op | Number
type Pair = { Items []*Node; Name string }

rule expr Node = (
    l:term rest:(op:term_binary_op r:term)*
) => foldl($l, $rest, (acc, i) => Op{Left: acc, Op: i.$op, Right: i.$r})

rule term_binary_op OpType = t:(("+" / "-") #atomic) => OpType{Text: text($t)}
rule group Node = "(" e:expr ")" => $e
rule number Number = t:([0-9]+ #atomic) => Number{Text: text($t)}
rule main = ^^ expr $$

rule e = operators {
    skip ws
    operand number / "(" e:e ")" => $e
    level { infix right "?" then:e ":" => Cond{Cond: $lhs, Then: $then, Else: $rhs} }
    level assignment { infix left "+" / "-" }
    level { prefix "-" => $rhs }
    level {
        postfix "(" args:level(e, assignment)? ")"
        postfix "!"
    }
}
`

func TestParseFile(t *testing.T) {
	g, err := Parse(calc)
	if err != nil {
		t.Fatal(err)
	}
	if g.File.Pkg.Name != "calc" {
		t.Errorf("package %q", g.File.Pkg.Name)
	}
	if n := len(g.Types()); n != 5 {
		t.Errorf("%d types", n)
	}
	if n := len(g.Rules()); n != 6 {
		t.Errorf("%d rules", n)
	}
	expr := g.Rules()[0]
	if expr.Pos != (grammar.Pos{Line: 16, Col: 1}) {
		t.Errorf("pos %v", expr.Pos)
	}
	if lam, ok := expr.Action.(*grammar.Call).Args[2].(*grammar.Lambda); !ok || !reflect.DeepEqual(lam.Params, []string{"acc", "i"}) {
		t.Errorf("lambda %#v", expr.Action)
	}
	pr := g.Rules()[5].Expr.(*grammar.OperatorsRule)
	if len(pr.Operands) != 1 || len(pr.Levels) != 4 || pr.Levels[1].Name != "assignment" {
		t.Errorf("operatorsRule %#v", pr)
	}
	if op := pr.Levels[0].Operators[0]; op.Kind != grammar.Infix || op.Assoc != grammar.AssocRight || op.Action == nil {
		t.Errorf("operator %#v", op)
	}
	if n := len(pr.Levels[3].Operators); n != 2 {
		t.Errorf("%d postfix operators", n)
	}
}

// Parsing the output of Format again yields the same grammar.
func TestFormatRoundTrip(t *testing.T) {
	g, err := Parse(calc)
	if err != nil {
		t.Fatal(err)
	}
	src := grammar.Format(g)
	g2, err := Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	if src2 := grammar.Format(g2); src != src2 {
		t.Errorf("not stable:\n%s\n---\n%s", src, src2)
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`rule a = `, `1:10: expected an expression, found end of file`},
		{`rule a = "abc`, `1:10: unterminated string`},
		{`rule a = [a-`, `1:10: unterminated character class`},
		{`rule a = []`, `1:10: expected an expression, found '[]'`},
		{`rule a = [z-a]`, `1:10: invalid range 'z'-'a'`},
		{`foo`, `1:1: expected 'rule', 'type' or 'var', found 'foo'`},
		{"rule a = (b\nrule c = d", `2:8: expected ')', found '='`},
		{`type A = { X }`, `1:14: expected identifier, found '}'`},
		{`type A struct { X }`, `1:8: a struct type is defined as type A = {...}; run pego fix to rewrite type A struct`},
		{`type A = B`, `1:8: expected '{' or 'node[' after '='; write type A ~ <type> for an alias (pego fix rewrites type A = <type>)`},
		{`type A B`, `1:8: expected '=' or '~', found 'B'`},
		{`type A = node[string, int]`, `a node type takes one type argument: type A = node[T]`},
		{`type A = node`, `1:8: expected '{' or 'node[' after '='`},
		{`rule a = operators { level { infix "+" } }`, `1:36: expected 'left', 'right', or 'none', found "+"`},
		{`rule a = x => A{X 1}`, `1:19: expected ',', found 1`},
		{"rule a = %\nrule b = c", `1:10: expected an expression, found '%'`},
		{"rule a = `", "1:10: unexpected character '`'"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			_, err := Parse(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v\nwant %s", err, tc.want)
			}
		})
	}
}

// After an error in one definition, parsing continues with the next one.
func TestParseErrorRecovery(t *testing.T) {
	_, err := Parse("rule a = (\nrule b = )\nrule c = ok")
	list, ok := err.(ErrorList)
	if !ok || len(list) != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestEscapes(t *testing.T) {
	g, err := Parse(`rule main = "\u{c}\u{b}\u{7}\u{8}" [\u{c}]`)
	if err != nil {
		t.Fatal(err)
	}
	seq := g.Rules()[0].Expr.(*grammar.Seq)
	if lit := seq.Items[0].(*grammar.Literal); lit.Value != "\f\v\a\b" {
		t.Errorf("literal %q", lit.Value)
	}
	if cc := seq.Items[1].(*grammar.CharClass); cc.Ranges[0].Lo != '\f' {
		t.Errorf("class %v", cc.Ranges)
	}
	for _, src := range []string{`rule main = "\q"`, `rule main = [\x41]`, `rule main = "\1"`} {
		if _, err := Parse(src); err == nil || !strings.Contains(err.Error(), "unknown escape sequence") {
			t.Errorf("%s: got %v", src, err)
		}
	}
}

func TestExprPositions(t *testing.T) {
	// Each prefix operator and atom records its own position; a postfix operator records where
	// its operand begins (the parenthesis of a group).
	e, err := ParseExpr("&a !b c #atomic -d _ _|_ #cut ^^ ^ $ $$ . (x y)* z+ (w)? v #repeat(2)")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range e.(*grammar.Seq).Items {
		var pos grammar.Pos
		switch it := it.(type) {
		case *grammar.And:
			pos = it.Diagnostic()
		case *grammar.Not:
			pos = it.Diagnostic()
		case *grammar.Atomic:
			pos = it.Diagnostic()
		case *grammar.Discard:
			pos = it.Diagnostic()
		case *grammar.Top:
			pos = it.Diagnostic()
		case *grammar.Bottom:
			pos = it.Diagnostic()
		case *grammar.Cut:
			pos = it.Diagnostic()
		case *grammar.BeginInput:
			pos = it.Diagnostic()
		case *grammar.BeginLine:
			pos = it.Diagnostic()
		case *grammar.EndLine:
			pos = it.Diagnostic()
		case *grammar.EndInput:
			pos = it.Diagnostic()
		case *grammar.Any:
			pos = it.Diagnostic()
		case *grammar.Repeat:
			pos = it.Diagnostic()
		case *grammar.Optional:
			pos = it.Diagnostic()
		default:
			t.Fatalf("unexpected %T", it)
		}
		got = append(got, fmt.Sprintf("%s@%d", grammar.FormatExpr(it), pos.Col))
	}
	want := "&a@1 !b@4 c #atomic@9 -d@17 _@20 _|_@22 #cut@26 ^^@31 ^@34 $@36 $$@38 .@41 (x y)*@43 z+@50 (w)?@53 v #repeat(2)@58"
	if s := strings.Join(got, " "); s != want {
		t.Errorf("got  %s\nwant %s", s, want)
	}
}

func TestParseRejectsTerminalType(t *testing.T) {
	const src = "type T terminal"
	_, err := Parse(src)
	list, ok := err.(ErrorList)
	if !ok || len(list) != 1 {
		t.Fatalf("got %v, want exactly one error", err)
	}
	const want = "a terminal type is the struct type T = { Text string }; run pego fix to rewrite type T terminal"
	if list[0].Msg != want || list[0].Pos != (grammar.Pos{Line: 1, Col: 8}) {
		t.Errorf("got %v, want 1:8: %s", err, want)
	}
	g, forms, err := parseLegacy(src)
	if err != nil {
		t.Fatalf("parseLegacy: %v", err)
	}
	td := g.File.Decls[0].(*grammar.TypeDef)
	if fields := td.Struct().Fields; len(fields) != 1 || fields[0].Name != "Text" || !forms.terminals[td] {
		t.Errorf("parseLegacy: spec is %#v, want the struct { Text string }", td)
	}
}

// A positional constructor parses to New.Args; the checker accepts it only for a node type, and
// the legacy reader still marks it for pego fix.
func TestParsePositionalConstructor(t *testing.T) {
	const src = "type T = { Text string }\nrule r T = \"x\" => T{\"x\"}"
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := g.File.Decls[1].(*grammar.RuleDef).Action.(*grammar.New); !ok || len(n.Args) != 1 || len(n.Fields) != 0 {
		t.Errorf("action is %#v, want a constructor with one argument", g.File.Decls[1].(*grammar.RuleDef).Action)
	}
	g, forms, err := parseLegacy(src)
	if err != nil {
		t.Fatalf("parseLegacy: %v", err)
	}
	if n, ok := g.File.Decls[1].(*grammar.RuleDef).Action.(*grammar.New); !ok || !forms.positional[n] {
		t.Errorf("parseLegacy: action is %#v, want a positional constructor", g.File.Decls[1].(*grammar.RuleDef).Action)
	}
}

// A constructor takes named fields or positional values, not both.
func TestParsePositionalConstructorArity(t *testing.T) {
	g, err := Parse("type T = node[string]\nrule r T = \"x\" => T{\"x\", \"y\"}")
	if err != nil {
		t.Fatal(err)
	}
	if n := g.File.Decls[1].(*grammar.RuleDef).Action.(*grammar.New); len(n.Args) != 2 {
		t.Errorf("got %d arguments, want 2", len(n.Args))
	}
	for _, src := range []string{
		"type T = node[string]\nrule r T = \"x\" => T{\"x\", A: \"y\"}",
		"type T = node[string]\nrule r T = \"x\" => T{A: \"y\", \"x\"}",
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("%q: mixed constructor accepted", src)
		}
	}
}

func TestParseRejectsRetiredTypeNames(t *testing.T) {
	replacements := map[string]string{
		"terminal": "node[string]",
		"Match":    "node[string]",
		"Seq":      "node[_]",
		"List":     "node[_]",
		"Operator": "node[_]",
		"Error":    "error",
	}
	for name, replacement := range replacements {
		for _, position := range []struct{ name, src string }{
			{"struct field", "type R = { F NAME }"},
			{"alias", "type R ~ NAME"},
			{"rule type", "rule r NAME = \"x\""},
			{"list", "type R ~ []NAME"},
			{"pointer", "type R = { F *NAME }"},
			{"join", "type R ~ int | NAME"},
			{"nested", "type R = { F []*NAME }"},
			{"type application", "type R = { F NAME[int] }"},
			{"cast", "rule r = x:\"x\" => $x.(NAME)"},
		} {
			t.Run(name+"/"+position.name, func(t *testing.T) {
				src := strings.Replace(position.src, "NAME", name, 1)
				_, err := Parse(src)
				list, ok := err.(ErrorList)
				if !ok || len(list) != 1 {
					t.Fatalf("got %v, want exactly one error", err)
				}
				want := fmt.Sprintf("%s is not a type; write %s (pego fix rewrites it)", name, replacement)
				col := strings.Index(src, name) + 1
				if list[0].Msg != want || list[0].Pos != (grammar.Pos{Line: 1, Col: col}) {
					t.Errorf("got %v, want 1:%d: %s", err, col, want)
				}
				if _, _, err := parseLegacy(src); err != nil {
					t.Errorf("parseLegacy: %v", err)
				}
			})
		}
	}
}

// node without a type argument is an error that pego fix rewrites to node[_].
func TestParseRejectsBareNode(t *testing.T) {
	for _, src := range []string{
		"type R = { F node }",
		"type R ~ []*node",
		"rule r node = \"x\"",
		"rule r = x:\"x\" => $x.(node)",
		"type R ~ node[string] | node",
	} {
		_, err := Parse(src)
		list, ok := err.(ErrorList)
		if !ok || len(list) != 1 || list[0].Msg != bareNode+" (pego fix rewrites it)" {
			t.Errorf("Parse(%q): got %v, want one error %q", src, err, bareNode)
		}
		fixed, err := Fix(src)
		if err != nil {
			t.Fatalf("Fix(%q): %v", src, err)
		}
		if want := strings.ReplaceAll(src, "node", "node[_]"); strings.Contains(src, "node[string]") {
			if want = strings.ReplaceAll(want, "node[_][string]", "node[string]"); fixed != want {
				t.Errorf("Fix(%q) = %q, want %q", src, fixed, want)
			}
		} else if fixed != want {
			t.Errorf("Fix(%q) = %q, want %q", src, fixed, want)
		}
		if _, err := Parse(fixed); err != nil {
			t.Errorf("Parse(Fix(%q)): %v", src, err)
		}
	}
}

func TestParseAcceptsValueTypeNames(t *testing.T) {
	for _, src := range []string{
		"type R = { F missing }",
		"type R = { F node[string] }",
		"type R = { F node[_] }",
		"type R = { F error }",
		"type R ~ missing | node[string] | node[_] | error",
		"type R ~ []node[string] | *error",
		"rule r node[string] = \"x\"",
		"rule r error = \"x\"",
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("Parse(%q): %v", src, err)
		}
	}
}

// A field may be named like a retired type: only type positions are rejected.
func TestParseAcceptsFieldsNamedLikeRetiredTypes(t *testing.T) {
	const src = `type R = { Match *node[string]; Operator node[_]; Seq node[_]; List []node[_]; Error error; terminal string }
rule main R = x:"a" => R{Match: nil, Operator: nil, Seq: nil, List: nil, Error: nil, terminal: "t"}
rule use = r:main => list($r.Match, $r.Operator)
`
	if _, err := Parse(src); err != nil {
		t.Fatal(err)
	}
}
