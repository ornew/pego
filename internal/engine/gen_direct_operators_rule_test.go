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

var typedOperatorsRuleBodyCases = []genCase{
	{"typed/OperatorsRule_associativity", `type Number terminal
type Binary = { Left Expr; Right Expr; Op Match }
type Unary = { X Expr; Op Match }
type Expr ~ Number | Binary | Unary
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    skip " "*
    operand number
    operand "(" e:expr ")" => $e
    level { infix none "=" => Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { infix left "+" / "-" => Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { prefix "-" => Unary{X: $rhs, Op: $op} }
    level { infix right "^" => Binary{Left: $lhs, Right: $rhs, Op: $op} }
    level { postfix "!" => Unary{X: $lhs, Op: $op} }
}
rule number Number = [0-9]+
`, []string{"1+2-3", "1^2^3", "-1^2!", "(1+2)!", "1=2", "1=2=3", "1+", "-", "1!+", "(1+2", " 1 + 2", ""}},
	{"typed/OperatorsRule_longest_ties", `type Atom terminal
type Prefix = { X Expr; Tag int }
type Expr ~ Atom | Prefix
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    operand atom
    level {
        prefix "-" => Prefix{X: $rhs, Tag: 1}
        prefix "--" => Prefix{X: $rhs, Tag: 2}
        prefix "--" => Prefix{X: $rhs, Tag: 3}
        prefix "¬" => Prefix{X: $rhs, Tag: 4}
    }
}
rule atom Atom = "-x" / "a" / "é"
`, []string{"a", "-a", "--a", "---a", "--", "-x", "¬é", "¬", "--x", ""}},
	{"typed/OperatorsRule_element_captures", `type Atom terminal
type Post = { X Expr; Items []Match; N int }
type Expr ~ Atom | Post
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    operand atom
    level {
        postfix "[" items:(x:[a-z] #atomic {len($x) == 1} -",") #repeat(1, 3) "]" => Post{X: $lhs, Items: map($items, (e) => e.$x), N: len($items)}
    }
}
rule atom Atom = "a"
`, []string{"a", "a[b,]", "a[b,c,][d,]", "a[]", "a[b,", "a[b,c,d,e,]", "a[é,]", ""}},
	{"typed/OperatorsRule_positional_actions", `type Atom terminal
type Doc = { Text Match; Items []node }
type Expr ~ Atom | Doc
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    operand atom
    operand v1:"[" "é" #atomic "]" => Doc{Text: $v1, Items: list($v1)}
    level { postfix "(" x:"x" #atomic ")" => Doc{Text: $x, Items: list($op)} }
}
rule atom Atom = "a"
`, []string{"a", "[é]", "[é](x)", "a(x)", "[é", "a(x", "a(y)", ""}},
	{"typed/OperatorsRule_environment_rollback", `type Atom terminal
type Prefix = { X Expr; Tag int; Text Match }
type Expr ~ Atom | Prefix
rule main Expr = {n := 0} e:expr $$ => $e
rule expr Expr = operators {
    skip " " {n := 8} "!" / " "*
    operand atom
    level {
        prefix o:"-" #atomic {n := 1} "x" "z" => Prefix{X: $rhs, Tag: n, Text: $o}
        prefix o:"-" #atomic {n := 2} "x" => Prefix{X: $rhs, Tag: n, Text: $o}
    }
}
rule atom Atom = "a"
`, []string{"a", "-xa", "-xza", " -xa", " !a", "-x", "-xz", " -x", "", "-x?a"}},
	{"typed/OperatorsRule_lookahead_memo", `type Atom terminal
type Prefix = { X Expr; Text Match }
type Expr ~ Atom | Prefix
rule main Expr = &(expr "!") e:expr "!" $$ => $e
rule expr Expr = operators {
    operand atom
    level { prefix &x:"¬" #atomic x:"¬" #atomic => Prefix{X: $rhs, Text: $x} }
}
rule atom Atom = "é"
`, []string{"é!", "¬é!", "¬¬é!", "¬!", "¬é?", "é!!", ""}},
	{"typed/OperatorsRule_nullable_parts", `type Atom terminal
type Prefix = { X Expr }
type Expr ~ Atom | Prefix
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    operand atom
    level { prefix "" => Prefix{X: $rhs} }
}
rule atom Atom = "a"?
`, []string{"", "a", "aa", "b"}},
	{"typed/OperatorsRule_framed_cuts", `type Doc = { Text node }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand x:"a" #atomic #cut => Doc{Text: $x}
    level { prefix "-" #cut "z" => Doc{Text: $op} }
}
`, []string{"a", "-za", "-a", "-z", "a?", "", "-x"}},
	{"typed/OperatorsRule_recovery", `type Doc = { Text Match | error }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand x:"a" #recover(skip=";") #atomic => Doc{Text: $x}
    level { prefix "-" #recover(skip=";") => Doc{Text: $op} }
}
`, []string{"a", ";", "-a", "a?", "", "-;"}},
	{"typed/OperatorsRule_unreachable_capture", `type Doc = { Text *Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand x:"a" #atomic / _|_ ignored:"b" #atomic => Doc{Text: $ignored}
}
`, []string{"a", "b", "", "a?"}},
	{"typed/OperatorsRule_mixed_routes", `type Doc = { Text *Match }
rule main Doc = e:expr $$ => $e
rule expr Doc = operators {
    operand x:"a" #atomic => Doc{Text: $x}
    operand x:"b" #atomic / _|_ ignored:"c" #atomic => Doc{Text: $ignored}
    operand x:"é" #atomic => Doc{Text: $x}
}
`, []string{"a", "b", "é", "c", "", "é?"}},
	{"typed/OperatorsRule_error_isolation", `type Atom terminal
type Prefix = { X Expr; Text Match }
type Expr ~ Atom | Prefix
rule main Expr = e:expr $$ => $e
rule expr Expr = operators {
    operand atom
    level { prefix (x:"-" #atomic "z") @error(message="prefix marker") => Prefix{X: $rhs, Text: $x} }
}
rule atom Atom = "a" @error(message="atom")
`, []string{"a", "-za", "-z", "-a", "-", "b", ""}},
}

func TestGeneratedTypedOperatorsRuleBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("builds generated code")
	}
	calculator, err := os.ReadFile("../../examples/calculator/calc.pego")
	if err != nil {
		t.Fatal(err)
	}
	cases := append([]genCase(nil), typedOperatorsRuleBodyCases...)
	cases = append(cases, genCase{"typed/OperatorsRule_calculator", string(calculator), []string{"1", "1+2*3", "2^3^4", "-(1+2)", "1+", "(1+2", "é", ""}})
	cases = append(cases, prefixPartCutCorpus())
	for _, c := range cases {
		g, err := syntax.Parse(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		opts := GenOptions{Package: "operatorsRulebody", Start: "main", Types: true, Recognize: true}
		code, err := Generate(g, opts)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(string(code), "tparse(trules") {
			t.Fatalf("%s: typed runtime is not exercised", c.name)
		}
		inlined := strings.Contains(string(code), "(typed OperatorsRule body inlined)")
		fallback := strings.HasSuffix(c.name, "_fallback")
		if inlined == fallback {
			t.Fatalf("%s: incorrect OperatorsRule body route (inline=%v)", c.name, inlined)
		}
		if c.name == "typed/OperatorsRule_mixed_routes" && strings.Count(string(code), "(typed OperatorsRule body inlined)") != 3 {
			t.Fatal("mixed routes: all three operands must use unfinished bodies")
		}
		opts.disableTypedOperatorsRuleBodies = true
		ref, err := Generate(g, opts)
		if err != nil || strings.Contains(string(ref), "(typed OperatorsRule body inlined)") || strings.Contains(string(code), "disableTypedOperatorsRuleBodies") {
			t.Fatalf("%s: invalid generation-only control: %v", c.name, err)
		}
		opts.Types = false
		nodeRef, err := Generate(g, opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.disableTypedOperatorsRuleBodies = false
		node, err := Generate(g, opts)
		if err != nil || !bytes.Equal(nodeRef, node) {
			t.Fatalf("%s: typed control changes Node output: %v", c.name, err)
		}
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{disableTypedOperatorsRuleBodies: true})
	testGeneratedTypesCorpus(t, goBin, cases, GenOptions{convertTypes: true})
}
