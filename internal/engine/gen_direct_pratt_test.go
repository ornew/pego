package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

var typedPrattBodyCases = []genCase{
	{"typed/Pratt_associativity", `
type Number terminal
type Binary struct { Left Expr, Right Expr, Op Match }
type Unary struct { X Expr, Op Match }
type Expr = Number | Binary | Unary
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    skip " "*
    operand number
    operand "(" e:expr ")" -> $e
    level { infix none "=" -> new Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { infix left "+" / "-" -> new Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { prefix "-" -> new Unary{X: $rhs, Op: $op} }
    level { infix right "^" -> new Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { postfix "!" -> new Unary{X: $lhs, Op: $op} }
}
def number: Number = (?0-9)+
`, []string{"1+2-3", "1^2^3", "-1^2!", "(1+2)!", "1=2", "1=2=3", "1+", "-", "1!+", "(1+2", " 1 + 2", ""}},
	{"typed/Pratt_longest_ties", `
type Atom terminal
type Prefix struct { X Expr, Tag int }
type Expr = Atom | Prefix
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    operand atom
    level {
        prefix "-" -> new Prefix{X: $rhs, Tag: 1}
        prefix "--" -> new Prefix{X: $rhs, Tag: 2}
        prefix "--" -> new Prefix{X: $rhs, Tag: 3}
        prefix "¬" -> new Prefix{X: $rhs, Tag: 4}
    }
}
def atom: Atom = "-x" / "a" / "é"
`, []string{"a", "-a", "--a", "---a", "--", "-x", "¬é", "¬", "--x", ""}},
	{"typed/Pratt_element_captures", `
type Atom terminal
type Post struct { X Expr, Items []Match, N int }
type Expr = Atom | Post
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    operand atom
    level { postfix "[" items:(x:@(?a-z) [len($x) == 1] -","){1,3} "]"
                -> new Post{X: $lhs, Items: map($items, (e) => $e.x), N: len($items)} }
}
def atom: Atom = "a"
`, []string{"a", "a[b,]", "a[b,c,][d,]", "a[]", "a[b,", "a[b,c,d,e,]", "a[é,]", ""}},
	{"typed/Pratt_positional_actions", `
type Atom terminal
type Doc struct { Text Match, Items []Match }
type Expr = Atom | Doc
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    operand atom
    operand "[" @"é" "]" -> new Doc{Text: $1, Items: list($1)}
    level { postfix "(" x:@"x" ")" -> new Doc{Text: $x, Items: list($op)} }
}
def atom: Atom = "a"
`, []string{"a", "[é]", "[é](x)", "a(x)", "[é", "a(x", "a(y)", ""}},
	{"typed/Pratt_environment_rollback", `
type Atom terminal
type Prefix struct { X Expr, Tag int, Text Match }
type Expr = Atom | Prefix
def main: Expr = [n = 0] e:expr $$ -> $e
def expr: Expr = pratt {
    skip (" " [n = 8] "!" / " "*)
    operand atom
    level {
        prefix o:@"-" [n = 1] "x" "z" -> new Prefix{X: $rhs, Tag: n, Text: $o}
        prefix o:@"-" [n = 2] "x" -> new Prefix{X: $rhs, Tag: n, Text: $o}
    }
}
def atom: Atom = "a"
`, []string{"a", "-xa", "-xza", " -xa", " !a", "-x", "-xz", " -x", "", "-x?a"}},
	{"typed/Pratt_lookahead_memo", `
type Atom terminal
type Prefix struct { X Expr, Text Match }
type Expr = Atom | Prefix
def main: Expr = &(expr "!") e:expr "!" $$ -> $e
def expr: Expr = pratt {
    operand atom
    level { prefix &(x:@"¬") x:@"¬" -> new Prefix{X: $rhs, Text: $x} }
}
def atom: Atom = "é"
`, []string{"é!", "¬é!", "¬¬é!", "¬!", "¬é?", "é!!", ""}},
	{"typed/Pratt_nullable_parts", `
type Atom terminal
type Prefix struct { X Expr }
type Expr = Atom | Prefix
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    operand atom
    level { prefix "" -> new Prefix{X: $rhs} }
}
def atom: Atom = "a"?
`, []string{"", "a", "aa", "b"}},
	{"typed/Pratt_cut_fallback", `
type Doc struct { Text Match }
def main: Doc = e:expr $$ -> $e
def expr: Doc = pratt {
    operand x:@"a" -- -> new Doc{Text: $x}
    level { prefix "-" -- "z" -> new Doc{Text: $op} }
}
`, []string{"a", "-za", "-a", "-z", "a?", "", "-x"}},
	{"typed/Pratt_recovery_fallback", `
type Doc struct { Text Match }
def main: Doc = e:expr $$ -> $e
def expr: Doc = pratt {
    operand x:@"a" #recover(skip=";") -> new Doc{Text: $x}
    level { prefix "-" #recover(skip=";") -> new Doc{Text: $op} }
}
`, []string{"a", ";", "-a", "a?", "", "-;"}},
	{"typed/Pratt_unreachable_capture", `
type Doc struct { Text *Match }
def main: Doc = e:expr $$ -> $e
def expr: Doc = pratt {
    operand x:@"a" / (_|_ ignored:@"b") -> new Doc{Text: $ignored}
}
`, []string{"a", "b", "", "a?"}},
	{"typed/Pratt_mixed_routes", `
type Doc struct { Text *Match }
def main: Doc = e:expr $$ -> $e
def expr: Doc = pratt {
    operand x:@"a" -> new Doc{Text: $x}
    operand x:@"b" / (_|_ ignored:@"c") -> new Doc{Text: $ignored}
    operand x:@"é" -> new Doc{Text: $x}
}
`, []string{"a", "b", "é", "c", "", "é?"}},
	{"typed/Pratt_error_isolation", `
type Atom terminal
type Prefix struct { X Expr, Text Match }
type Expr = Atom | Prefix
def main: Expr = e:expr $$ -> $e
def expr: Expr = pratt {
    operand atom
    level { prefix (x:@"-" "z") #error(message="prefix marker") -> new Prefix{X: $rhs, Text: $x} }
}
def atom: Atom = "a" #error(message="atom")
`, []string{"a", "-za", "-z", "-a", "-", "b", ""}},
}

func TestGeneratedTypedPrattBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	calculator, err := os.ReadFile("../../examples/calculator/calc.pego")
	if err != nil {
		t.Fatal(err)
	}
	cases := append([]genCase(nil), typedPrattBodyCases...)
	cases = append(cases, genCase{"typed/Pratt_calculator", string(calculator), []string{"1", "1+2*3", "2^3^4", "-(1+2)", "1+", "(1+2", "é", ""}})
	cases = append(cases, prefixPartCutCorpus())
	for _, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := GenOptions{Package: "prattbody", Start: "main", Types: true, Recognize: true}
		code, err := Generate(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(string(code), "tparse(trules") {
			t.Fatalf("%s: typed runtime is not exercised", c.name)
		}
		inlined := strings.Contains(string(code), "(typed Pratt body inlined)")
		fallback := strings.HasSuffix(c.name, "_fallback") || c.name == "typed/Pratt_unreachable_capture"
		if inlined == fallback {
			t.Fatalf("%s: incorrect Pratt body route (inline=%v)", c.name, inlined)
		}
		if c.name == "typed/Pratt_mixed_routes" && strings.Count(string(code), "(typed Pratt body inlined)") != 2 {
			t.Fatal("mixed routes: rejected body must preserve the two eligible lines")
		}
		opts.disableTypedPrattBodies = true
		ref, err := Generate(g, opts)
		if err != nil || strings.Contains(string(ref), "(typed Pratt body inlined)") || strings.Contains(string(code), "disableTypedPrattBodies") {
			t.Fatalf("%s: invalid generation-only control: %v", c.name, err)
		}
		opts.Types = false
		nodeRef, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedPrattBodies = false
		node, err := Generate(g, opts)
		if err != nil || !bytes.Equal(nodeRef, node) {
			t.Fatalf("%s: typed control changes Node output: %v", c.name, err)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{disableTypedPrattBodies: true})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true})
}
