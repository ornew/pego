package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// A newline ends a statement, and an expression crosses a newline only inside
// its own open delimiter.
func TestStatementBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		pos, msg  string // empty when the source is accepted
	}{
		{"struct fields on lines", "type T = {\n a string\n b string\n}\n", "", ""},
		{"struct fields with semicolons", "type T = { a string; b string }\n", "", ""},
		{"struct field semicolon then newline", "type T = { a string;\n b string }\n", "", ""},
		{"struct trailing semicolon", "type T = { a string; }\n", "", ""},
		{"action statements with semicolons", "rule a = b => {x; y}\n", "", ""},
		{"action statement semicolon then newline", "rule a = b => {x;\n y}\n", "", ""},
		{"operators items with semicolons", "rule a = operators { operand \"x\"; operand \"y\" }\n", "", ""},
		{"double semicolon in a struct", "type T = { a string;; b string }\n", "1:21", "expected identifier"},
		{"semicolon on its own line in a struct", "type T = {\n a string\n ;\n b string\n}\n", "3:2", "expected identifier"},
		{"semicolon after a comment in a struct", "type T = { a string // t\n; b string }\n", "2:1", "expected identifier"},
		{"semicolon on its own line in an action block", "rule a = b => {\n x\n ;\n y\n}\n", "3:2", ""},
		{"semicolon after a comment in an action block", "rule a = b => {x // t\n; y}\n", "2:1", ""},
		{"newline between = and { of a type", "type T =\n{ a string }\n", "2:1", "multiline expressions require their own open delimiter"},
		{"comment between = and { of a type", "type T = // c\n{ a string }\n", "2:1", "multiline expressions require their own open delimiter"},
		{"newline between => and an action block", "rule a = b =>\n{x; y}\n", "2:1", "multiline expressions require their own open delimiter"},
		{"newline between => and an action", "rule a = b =>\nx\n", "2:1", "multiline expressions require their own open delimiter"},
		{"double semicolon at the top level", "rule a = x;; rule b = y\n", "1:12", "found ';'"},
		{"semicolon on its own line at the top level", "rule a = x\n;\nrule b = y\n", "2:1", "found ';'"},
		{"semicolon after a comment at the top level", "rule a = x // t\n; rule b = y\n", "2:1", "found ';'"},
		{"double semicolon in operators", "rule a = operators { operand \"x\";; operand \"y\" }\n", "1:34", "found ';'"},
		{"semicolon on its own line in a level", "rule a = operators { level { prefix \"-\"\n; postfix \"!\" } }\n", "2:1", "found ';'"},
		{"newline before a binary operator", "rule a = b => x\n+ 1\n", "2:1", ""},
		{"newline before a member selector", "rule a = b => x\n.Foo\n", "2:1", ""},
		{"newline before a constructor brace", "rule a = b => N\n{A: 1}\n", "2:1", ""},
		{"newline before a lambda arrow", "rule a = b => (x)\n=> x\n", "2:1", ""},
		{"newline before an index", "rule a = b => x\n[0]\n", "2:1", ""},
		{"newline before an operator inside a group", "rule a = b => (x\n+ 1)\n", "", ""},
		{"newline inside an index", "rule a = b => x[\n0\n]\n", "", ""},
		{"newline before a lambda arrow inside a group", "rule a = b => ((x)\n=> x)\n", "", ""},
		{"index on a list literal", "rule a = b => []int{1, 2}[0]\n", "", ""},
		{"selector on a list literal", "rule a = b => []int{1, 2}.Foo\n", "", ""},
		{"index after white space", "rule a = b => x [0]\n", "1:17", "an index [ must directly follow"},
		{"index after a comment", "rule a = b => x /* c */ [0]\n", "1:25", "an index [ must directly follow"},
		{"index after a constructor and white space", "rule a = b => N{} [0]\n", "1:19", "an index [ must directly follow"},
		{"predicate then character class", "rule a = {x}[a-z]\n", "", ""},
		{"newline between operators and {", "rule a = operators\n{ operand \"x\" }\n", "2:1", ""},
		{"newline between => and an action inside a group", "rule a = (b =>\n{x; y})\n", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.src)
			if tc.pos == "" {
				if err != nil {
					t.Fatalf("rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.HasPrefix(err.Error(), tc.pos+":") || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("got %q, want position %s and message %q", err, tc.pos, tc.msg)
			}
		})
	}
}

// A "[" directly after a block is a character class in a rule body, and an
// index only after the "}" of a value inside an action.
func TestBracketAfterBlock(t *testing.T) {
	for _, src := range []string{
		"rule a = {x}[a-z]\n",
		"rule a = &{x}[a-z]\n",
		"rule a = b {x}[a-z]\n",
		"rule a = b{x}[a-z]\n",
		"rule a = (b => N{}) {x}[a-z]\n",
		"rule a = b => N{}\nrule c = {x}[a-z]\n",
		"rule a = b => {x}\nrule c = d{x}[a-z]\n",
		"rule a = {x}[a-z] => N{}\n",
		"rule a = operators { operand {x}[a-z] => N{} }\n",
		"type T = { A int }\nrule a = {x}[a-z]\n",
	} {
		g, err := Parse(src)
		if err != nil {
			t.Errorf("%q rejected: %v", src, err)
			continue
		}
		found := false
		for _, r := range g.Rules() {
			found = found || strings.Contains(grammar.FormatExpr(r.Expr), "[a-z]")
		}
		if !found {
			t.Errorf("%q: the character class is lost", src)
		}
	}
	for _, src := range []string{
		"rule a = b => N{}[0]\n",
		"rule a = b => []int{1}[0]\n",
		"rule a = (b => []int{1}[0])\n",
		"rule a = b => {x := []int{1}[0]; x}\n",
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("%q rejected: %v", src, err)
		}
	}
}
