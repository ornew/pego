package syntax

import (
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
		{`a* b+ c? d{2} e{2,} f{2,3} g{,3}`, `a* b+ c? d{2} e{2,} f{2,3} g{0,3}`},
		{`&a !b @c -d`, `&a !b @c -d`},
		{`x:a y:(b c)* z:@d`, `x:a y:(b c)* z:@d`},
		{`!"if" (?a-z)+`, `!"if" (?a-z)+`},
		{`(?0-9a-zA-Z_)`, `(?0-9a-zA-Z_)`},
		{`(?\-\(?\))`, `(?\-\(\?\))`},
		{`(?^"\\)`, `(?^"\\)`},
		{`_ _|_ -- ^^ ^ $ $$ .`, `_ _|_ -- ^^ ^ $ $$ .`},
		{`expr(assignment) expr (a)`, `expr(assignment) expr a`},
		{`[len($s) > indent] [indent = len($s)]`, `[len($s) > indent] [indent = len($s)]`},
		{`[a + b * c == 1 && !x || y]`, `[a + b * c == 1 && !x || y]`},
		{`[(a + b) * -c]`, `[(a + b) * -c]`},
		{`"hello" / _|_ #error(message="expect 'hello'")`, `"hello" / _|_ #error(message="expect 'hello'")`},
		{`a* #stream #x`, `a* #stream #x`},
		{"a\n  // comment\n  b", `a b`},
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
	if _, ok := rep.Expr.(*grammar.Seq); !ok {
		t.Errorf("repeat body %T", rep.Expr)
	}
}

const calc = `
package calc

// --- Type definitions ---
type Number terminal
type OpType terminal

type Op struct {
    Left  Node
    Op    OpType
    Right Node
}

type Node = Op | Number
type Pair struct { Items []*Node, Name string }

def expr: Node =
    l:term rest:(op:term_binary_op r:term)*
    -> foldl($l, $rest, (acc, i) => new Op{Left: $acc, Op: $i.op, Right: $i.r})

def term_binary_op: OpType = @("+" / "-")
def group: Node = "(" e:expr ")" -> $e
def number: Number = @((?0-9)+)
def main = ^^ expr $$

def e = pratt {
    skip ws
    operand number / "(" e:e ")" -> $e
    level { infix right "?" then:e ":" -> new Cond{Cond: $lhs, Then: $then, Else: $rhs} }
    level assignment { infix left "+" / "-" }
    level { prefix "-" -> $rhs }
    level {
        postfix "(" args:e(assignment)? ")"
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
	if expr.Pos != (grammar.Pos{Line: 17, Col: 1}) {
		t.Errorf("pos %v", expr.Pos)
	}
	if lam, ok := expr.Action.(*grammar.Call).Args[2].(*grammar.Lambda); !ok || !reflect.DeepEqual(lam.Params, []string{"acc", "i"}) {
		t.Errorf("lambda %#v", expr.Action)
	}
	pr := g.Rules()[5].Expr.(*grammar.Pratt)
	if len(pr.Operands) != 1 || len(pr.Levels) != 4 || pr.Levels[1].Name != "assignment" {
		t.Errorf("pratt %#v", pr)
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
		{`def a = `, `1:9: expected an expression, found end of file`},
		{`def a = "abc`, `1:9: unterminated string`},
		{`def a = (?a-`, `1:9: unterminated character class`},
		{`def a = (?)`, `1:9: empty character class`},
		{`def a = (?z-a)`, `1:9: invalid range 'z'-'a'`},
		{`def a = b{3,1}`, `1:14: invalid repetition {3,1}`},
		{`foo`, `1:1: expected 'def' or 'type', found 'foo'`},
		{"def a = (b\ndef c = d", `2:1: expected ')', found 'def'`},
		{`type A struct { X }`, `1:19: expected identifier, found '}'`},
		{`def a = pratt { level { infix "+" } }`, `1:31: expected 'left', 'right', or 'none', found "+"`},
		{`def a = x -> new A{X 1}`, `1:22: expected ':', found 1`},
		{"def a = %\ndef b = c", `1:9: expected an expression, found '%'`},
		{"def a = `", "1:9: unexpected character '`'"},
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
	_, err := Parse("def a = (\ndef b = )\ndef c = ok")
	list, ok := err.(ErrorList)
	if !ok || len(list) != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestEscapes(t *testing.T) {
	g, err := Parse(`def main = "\f\v\a\b" (?\f)`)
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
	for _, src := range []string{`def main = "\q"`, `def main = (?\x41)`, `def main = "\1"`} {
		if _, err := Parse(src); err == nil || !strings.Contains(err.Error(), "unknown escape sequence") {
			t.Errorf("%s: got %v", src, err)
		}
	}
}
