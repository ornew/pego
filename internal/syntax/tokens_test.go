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
