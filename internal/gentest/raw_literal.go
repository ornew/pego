package gentest

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/compiletest"
)

// Replace a source-valid placeholder with a public-AST value. Only matching
// expressions are visited: string constants in actions have separate semantics.
func FirstLiteral(e grammar.Expr) *grammar.Literal {
	switch e := e.(type) {
	case *grammar.Group:
		return FirstLiteral(e.Expr)
	case *grammar.Literal:
		return e
	case *grammar.Seq:
		for _, item := range e.Items {
			if lit := FirstLiteral(item); lit != nil {
				return lit
			}
		}
	case *grammar.Choice:
		for _, item := range e.Alts {
			if lit := FirstLiteral(item); lit != nil {
				return lit
			}
		}
	case *grammar.Capture:
		return FirstLiteral(e.Expr)
	case *grammar.Atomic:
		return FirstLiteral(e.Expr)
	case *grammar.Repeat:
		return FirstLiteral(e.Expr)
	}
	return nil
}

// ReplaceFirstLiteral sets the value of the first literal of e and returns it, or nil if there is none.
func ReplaceFirstLiteral(e grammar.Expr, val string) *grammar.Literal {
	lit := FirstLiteral(e)
	if lit != nil {
		lit.Value = val
	}
	return lit
}

// RawLiteralValues are literal values that are not valid UTF-8, which the raw literals of the tests
// carry.
func RawLiteralValues() []string {
	return []string{"\xc3", "\x80", "\xff", "\xc0\xaf", "\xed\xa0\x80", "é\xc3x", "abcdefghijk\xffxyz"}
}

// RawLiteralGenCorpus returns the cases of the generators with raw literal values.
func RawLiteralGenCorpus() []Case {
	var cases []Case
	for _, raw := range RawLiteralValues() {
		canonical := string([]rune(raw))
		for _, shape := range []string{"exact", "suffix", "choice", "predicate"} {
			expr := strconv.Quote(canonical)
			predicate := ""
			switch shape {
			case "suffix":
				expr = "(" + expr + " .) #atomic"
			case "predicate":
				predicate = " {text($t)==" + strconv.Quote(canonical) + "}"
			case "choice":
				expr = "(" + expr + ` / "other")`
			}
			cases = append(cases, Case{Name: fmt.Sprintf("raw AST/%x/%s", raw, shape),
				Src:    "type N = { T node[string] }\nrule main N = t:" + expr + predicate + " $$ => N{T:$t}",
				Inputs: []string{raw, canonical, raw + "x", canonical + "x", "é", "éx", "other", "", "\xffx", "\x80x"}})
		}
	}
	return cases
}

// LiteralCorpusGrammar parses the source of the case tc, and for a raw AST case sets the literal to the raw value the name of the case gives.
func LiteralCorpusGrammar(t *testing.T, tc Case) *grammar.Grammar {
	t.Helper()
	g, err := compiletest.Parse(tc.Src)
	if err != nil {
		t.Fatalf("%s: %v", tc.Name, err)
	}
	if strings.HasPrefix(tc.Name, "raw AST/") {
		for _, raw := range RawLiteralValues() {
			if strings.HasPrefix(tc.Name, fmt.Sprintf("raw AST/%x/", raw)) {
				if ReplaceFirstLiteral(g.Rules()[0].Expr, raw) == nil {
					t.Fatal("missing literal")
				}
				return g
			}
		}
		t.Fatal("unknown raw literal case")
	}
	return g
}

// InvalidLiteralCorpus returns the cases of literals that match bytes which are not valid UTF-8.
func InvalidLiteralCorpus() []Case {
	cases := []Case{
		{Name: "text predicate", Src: `type N = { T node[string] }
rule main N = t:"�" {len(text($t)) == len($t)} $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "é"}},
		{Name: "replacement", Src: `type N = { T node[string] }
rule main N = t:"�" $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "é", "", "\xffx"}},
		{Name: "multiple", Src: `type N = { T node[string] }
rule main N = t:"���" $$ => N{T: $t}`, Inputs: []string{"\xed\xa0\x80", "\xe0\x80\x80", "���", "\xff", "\xff\x80\x80x"}},
		{Name: "mixed", Src: `type N = { T node[string] }
rule main N = t:"é�x" $$ => N{T: $t}`, Inputs: []string{"é\xffx", "é\xc3x", "é�x", "é\xffy", "é\xff", "éx"}},
		{Name: "long", Src: `type N = { T node[string] }
rule main N = t:"abcdefghijk�xyz" $$ => N{T: $t}`, Inputs: []string{"abcdefghijk\xffxyz", "abcdefghijk�xyz", "abcdefghijk\xc3xyz", "abcdefghijk\xffxyq"}},
		{Name: "choice", Src: `type N = { T node[string] }
rule main N = t:("x" / "�") $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "x", "y"}},
		{Name: "class", Src: `type N = { T node[string] }
rule main N = t:[�] $$ => N{T: $t}`, Inputs: []string{"\xff", "\x80", "\xc3", "�", "é"}},
	}
	return append(cases, RawLiteralGenCorpus()...)
}
