package syntax

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestTokenize(t *testing.T) {
	src := "// doc\ndef 名前 = \"a😀\" x:(?a-z)* // end\n    -> $x $1\n// last"
	var got []string
	for _, tok := range Tokenize(src) {
		got = append(got, fmt.Sprintf("%d %q %d:%d-%d:%d", tok.Kind, tok.Text, tok.Pos.Line, tok.Pos.Col, tok.End.Line, tok.End.Col))
	}
	want := []string{
		`8 "// doc" 1:1-1:7`,
		`1 "def" 2:1-2:4`,
		`1 "名前" 2:5-2:7`,
		`7 "=" 2:8-2:9`,
		`2 "a😀" 2:10-2:14`,
		`1 "x" 2:15-2:16`,
		`7 ":" 2:16-2:17`,
		`4 "" 2:17-2:23`,
		`7 "*" 2:23-2:24`,
		`8 "// end" 2:25-2:31`,
		`7 "->" 3:5-3:7`,
		`5 "x" 3:8-3:10`,
		`6 "1" 3:11-3:13`,
		`8 "// last" 4:1-4:8`,
	}
	if g, w := strings.Join(got, "\n"), strings.Join(want, "\n"); g != w {
		t.Errorf("got\n%s\nwant\n%s", g, w)
	}
}

func TestTokenizeSkipsInvalidCharacters(t *testing.T) {
	toks := Tokenize("a ` b")
	if len(toks) != 2 || toks[0].Text != "a" || toks[1].Text != "b" || toks[1].Pos != (grammar.Pos{Line: 1, Col: 5}) {
		t.Errorf("got %+v", toks)
	}
}

func TestParsePartial(t *testing.T) {
	src := "def a = b\ndef broken = (\ndef b = \"x\"\ntype T struct { A }\ntype U terminal\n"
	g, errs := ParsePartial(src)
	if len(errs) != 2 {
		t.Fatalf("errors: %v", errs)
	}
	var names []string
	for _, s := range g.Statements {
		switch s := s.(type) {
		case *grammar.RuleDef:
			names = append(names, s.Name)
		case *grammar.TypeDef:
			names = append(names, s.Name)
		}
	}
	if got := strings.Join(names, " "); got != "a b U" {
		t.Errorf("statements %q", got)
	}
	if _, err := Parse(src); err == nil {
		t.Error("Parse accepted the source")
	}
	if _, errs := ParsePartial("def a = b"); errs != nil {
		t.Errorf("errors for valid source: %v", errs)
	}
}

func TestIsIdentifier(t *testing.T) {
	for s, want := range map[string]bool{
		"a": true, "_x1": true, "名前": true, "Abc": true,
		"": false, "1a": false, "a-b": false, "a b": false, "\xff": false, "$a": false,
	} {
		if got := IsIdentifier(s); got != want {
			t.Errorf("IsIdentifier(%q) = %v", s, got)
		}
	}
	if !IsKeyword("def") || IsKeyword("main") {
		t.Error("IsKeyword")
	}
}

// TestLongRunsOfInvalidCharacters checks that the lexer handles a long run of characters that
// start no token without recursing per character (which overflowed the stack), and reports the
// run as one error.
func TestLongRunsOfInvalidCharacters(t *testing.T) {
	junk := strings.Repeat("`;~😀", 1<<19) // 2M characters
	src := "def a = \"x\"\n" + junk + "\ndef b = \"y\""
	g, errs := ParsePartial(src)
	if len(errs) != 1 || errs[0].Pos != (grammar.Pos{Line: 2, Col: 1}) || !strings.HasPrefix(errs[0].Msg, "unexpected characters") {
		t.Errorf("errors %v", errs[:min(len(errs), 3)])
	}
	if len(g.Rules()) != 2 {
		t.Errorf("rules %v", g.Rules())
	}
	if toks := Tokenize(src); len(toks) != 8 {
		t.Errorf("%d tokens", len(toks))
	}
}

// TestErrorLimit checks that the number of errors is limited: many separate invalid characters
// make at most maxErrors errors and a last one that says there are more.
func TestErrorLimit(t *testing.T) {
	_, errs := ParsePartial(strings.Repeat("` ", 10000))
	if len(errs) != maxErrors+1 || errs[maxErrors].Msg != "too many errors" {
		t.Errorf("%d errors, the last %v", len(errs), errs[len(errs)-1])
	}
}

// TestDeepNesting checks that deeply nested expressions, types and values are reported as an
// error instead of overflowing the stack.
func TestDeepNesting(t *testing.T) {
	const n = 1 << 14 // far beyond the limit
	for _, src := range []string{
		"def a = " + strings.Repeat("(\n", 1<<20), // used to overflow the stack
		"def a = " + strings.Repeat("!", n) + "b",
		"def a = " + strings.Repeat("x:", n) + "b",
		"def a = b #recover(skip=" + strings.Repeat("c #recover(skip=", n),
		"def a = b -> " + strings.Repeat("- ", n) + "1",
		"def a = b -> " + strings.Repeat("(", n) + "1",
		"def a = b [" + strings.Repeat("!", n) + "x]",
		"def a = b -> foldl(" + strings.Repeat("(x) => foldl(", n),
		"def a: " + strings.Repeat("[]", n) + "T = b",
		"type T = " + strings.Repeat("(*", n) + "U",
	} {
		_, errs := ParsePartial(src + "\ndef ok = \"x\"")
		found := false
		for _, e := range errs {
			found = found || e.Msg == "nesting too deep"
		}
		if !found {
			t.Errorf("%.30q...: errors %v", src, errs[:min(len(errs), 3)])
		}
	}
	// Deep nesting below the limit is fine.
	if _, errs := ParsePartial("def a = " + strings.Repeat("(", 200) + "b" + strings.Repeat(")", 200)); errs != nil {
		t.Errorf("errors %v", errs)
	}
}

// TestIncompletePackageClause checks that a package clause without a name is reported as an
// error, not a panic, and that the definitions after it are parsed.
func TestIncompletePackageClause(t *testing.T) {
	for _, src := range []string{"package", "package ", "package {", "package\ndef a = \"x\"", "package 1 def a = \"x\""} {
		g, errs := ParsePartial(src)
		if len(errs) != 1 || !strings.HasPrefix(errs[0].Msg, "expected identifier") {
			t.Errorf("%q: errors %v", src, errs)
		}
		if strings.Contains(src, "def a") && (len(g.Statements) != 1 || g.Rules()[0].Name != "a") {
			t.Errorf("%q: statements %v", src, g.Statements)
		}
		if _, err := Parse(src); err == nil {
			t.Errorf("%q: Parse accepted it", src)
		}
	}
}
