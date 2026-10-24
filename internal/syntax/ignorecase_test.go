package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

// ruleExpr returns the expression of the rule main of the parsed grammar.
func ruleExpr(t *testing.T, src string) grammar.Expr {
	t.Helper()
	g, err := Parse(src)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	for _, d := range g.File.Decls {
		if rd, ok := d.(*grammar.RuleDef); ok && rd.Name == "main" {
			return rd.Expr
		}
	}
	t.Fatalf("no rule main in %q", src)
	return nil
}

func TestIgnoreCaseSuffix(t *testing.T) {
	lit, ok := ruleExpr(t, `rule main = "select"i`).(*grammar.Literal)
	if !ok || lit.Value != "select" || !lit.IgnoreCase {
		t.Fatalf("suffix literal: %#v", lit)
	}
	// A space before i, or an identifier that starts with i, makes a rule reference.
	for _, src := range []string{
		"rule main = \"a\" i\nrule i = \"x\"",
		"rule main = \"a\"ia\nrule ia = \"x\"",
		"rule main = \"a\"i_\nrule i_ = \"x\"",
		"rule main = \"a\"i2\nrule i2 = \"x\"",
	} {
		seq, ok := ruleExpr(t, src).(*grammar.Seq)
		if !ok || len(seq.Items) != 2 {
			t.Fatalf("%q: not a sequence of two items", src)
		}
		if l, ok := seq.Items[0].(*grammar.Literal); !ok || l.IgnoreCase {
			t.Fatalf("%q: first item %#v, want a case-sensitive literal", src, seq.Items[0])
		}
		if _, ok := seq.Items[1].(*grammar.Ref); !ok {
			t.Fatalf("%q: second item %#v, want a reference", src, seq.Items[1])
		}
	}
}

func TestIgnoreCasePragma(t *testing.T) {
	a, ok := ruleExpr(t, `rule main = ("a" "b") #ignorecase`).(*grammar.Attributed)
	if !ok || len(a.Attrs) != 1 || a.Attrs[0].Name != "ignorecase" || !a.Attrs[0].Pragma {
		t.Fatalf("pragma: %#v", ruleExpr(t, `rule main = ("a" "b") #ignorecase`))
	}
	for _, src := range []string{
		`rule main = "a" #ignorecase(x)`,
		`rule main = "a" #ignorecase()`,
	} {
		if _, err := Parse(src); err == nil || !strings.Contains(err.Error(), "#ignorecase takes no arguments") {
			t.Fatalf("%q: %v", src, err)
		}
	}
	if _, err := Parse(`rule main = "a" # ignorecase`); err == nil || !strings.Contains(err.Error(), "pragma name must immediately follow") {
		t.Fatalf("space after #: %v", err)
	}
}

func TestIgnoreCaseFormatAndJSON(t *testing.T) {
	src := "rule main = \"select\"i (\"from\" \"where\") #ignorecase\n"
	out := checkFormat(t, src)
	if !strings.Contains(out, `"select"i`) || !strings.Contains(out, "#ignorecase") {
		t.Fatalf("formatted output lost the suffix or the pragma:\n%s", out)
	}
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ignoreCase": true`) {
		t.Fatalf("JSON lacks the suffix flag:\n%s", data)
	}
	back, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := grammar.MarshalJSON(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("JSON round trip changed the grammar:\n%s\nwant:\n%s", again, data)
	}
}
