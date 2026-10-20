package spec_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

// literalNameSources declare a name that would hide the literal true, false or nil.
var literalNameSources = []struct{ name, src, want string }{
	{"rule", "rule true = \"a\"", "rule name true is reserved; it is a literal"},
	{"rule false", "rule main = \"a\"\nrule false = \"a\"", "rule name false is reserved; it is a literal"},
	{"variable", "var nil int\nrule main = \"a\"", "variable name nil is reserved; it is a literal"},
	{"variable with a value", "var true = true\nrule main = \"a\"", "variable name true is reserved; it is a literal"},
	{"type", "type nil = { N int }\nrule main = \"a\"", "type name nil is reserved; it is a literal"},
	{"alias", "type true ~ int\nrule main = \"a\"", "type name true is reserved; it is a literal"},
	{"capture", "rule main = true:\"a\" => $true", "capture name true is reserved; it is a literal"},
	{"capture in a group", "rule main = (x:\"a\" nil:\"b\") => $x", "capture name nil is reserved; it is a literal"},
}

// TestLiteralNamesAreReserved checks that no declaration can take the name of a literal, and that
// the hand-written frontend and the canonical grammar agree on every such source.
func TestLiteralNamesAreReserved(t *testing.T) {
	p, _ := sourceGrammar(t)
	for _, tc := range literalNameSources {
		t.Run(tc.name, func(t *testing.T) {
			g, err := pego.ParseGrammar(tc.src)
			if err != nil {
				t.Fatalf("hand-written frontend: %v", err)
			}
			if _, err := p.Parse(tc.src); err != nil {
				t.Fatalf("canonical frontend rejects the source the hand-written one accepts: %v", err)
			}
			if err := grammar.Validate(g); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate: got %v; want %q", err, tc.want)
			}
			if _, err := pego.CompileSource(tc.src, "main"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CompileSource: got %v; want %q", err, tc.want)
			}
		})
	}
	// A lambda parameter is rejected where it is parsed, by both frontends.
	for _, src := range []string{
		"rule main = x:\"a\" => map($x, (false) => false)", "rule main = x:\"a\" => map($x, (a, nil) => a)",
	} {
		if _, err := pego.ParseGrammar(src); err == nil || !strings.Contains(err.Error(), "cannot name a lambda parameter") {
			t.Errorf("hand-written frontend: %s: %v", src, err)
		}
		if _, err := p.Parse(src); err == nil {
			t.Errorf("canonical frontend accepts %s", src)
		}
	}
	// A grammar built by hand is validated like a parsed one.
	lambda := &grammar.Lambda{Params: []string{"a", "true"}, Body: &grammar.IntLit{Value: 1}}
	g := &grammar.Grammar{File: grammar.File{Decls: []grammar.Decl{&grammar.RuleDef{Name: "main", Expr: &grammar.Literal{Value: "a"}, Action: lambda}}}}
	if err := grammar.Validate(g); err == nil || !strings.Contains(err.Error(), "parameter name true is reserved; it is a literal") {
		t.Errorf("lambda built by hand: %v", err)
	}
	// Names that merely contain a literal are ordinary.
	for _, src := range []string{
		"rule truely = \"a\"", "var nilable bool\nrule main = \"a\"", "rule main = true_:\"a\" => $true_",
		"type True = { N int }", "rule main = x:\"a\" => map($x, (nilx) => nilx)",
	} {
		g, err := pego.ParseGrammar(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if err := grammar.Validate(g); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}
