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
		{`a* b+ c? d #repeat(2) e #repeat(2, ) f #repeat(2, 3) g #repeat(0, 3)`, `a* b+ c? d #repeat(2) e #repeat(2, ) f #repeat(2, 3) g #repeat(0, 3)`},
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
type Number terminal
type OpType terminal

type Op = {
    Left  Node
    Op    OpType
    Right Node
}

type Node ~ Op | Number
type Pair = { Items []*Node; Name string }

rule expr Node = (
    l:term rest:(op:term_binary_op r:term)*
) => foldl($l, $rest, (acc, i) => Op{Left: acc, Op: i.op, Right: i.r})

rule term_binary_op OpType = ("+" / "-") #atomic
rule group Node = "(" e:expr ")" => $e
rule number Number = [0-9]+ #atomic
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
	if g.Package != "calc" {
		t.Errorf("package %q", g.Package)
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
		{`foo`, `1:1: expected 'rule' or 'type', found 'foo'`},
		{"rule a = (b\nrule c = d", `2:8: expected ')', found '='`},
		{`type A = { X }`, `1:14: expected identifier, found '}'`},
		{`type A struct { X }`, `1:8: a struct type is defined as type A = {...}; run pego fix to rewrite type A struct`},
		{`type A = B`, `1:8: only struct types can be defined with =; write type A ~ <type> for an alias (pego fix rewrites type A = <type>)`},
		{`type A B`, `1:8: expected '=', '~', or 'terminal', found 'B'`},
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
