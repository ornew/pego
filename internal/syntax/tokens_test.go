package syntax

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

func TestTokenize(t *testing.T) {
	src := "// doc\nrule ab = \"a😀\" x:[a-z]* // end\n    => $x $1\n// last"
	var got []string
	for _, tok := range Tokenize(src) {
		got = append(got, fmt.Sprintf("%d %q %d:%d-%d:%d", tok.Kind, tok.Text, tok.Pos.Line, tok.Pos.Col, tok.End.Line, tok.End.Col))
	}
	want := []string{
		`8 "// doc" 1:1-1:7`,
		`1 "rule" 2:1-2:5`,
		`1 "ab" 2:6-2:8`,
		`7 "=" 2:9-2:10`,
		`2 "a😀" 2:11-2:15`,
		`1 "x" 2:16-2:17`,
		`7 ":" 2:17-2:18`,
		`4 "" 2:18-2:23`,
		`7 "*" 2:23-2:24`,
		`8 "// end" 2:25-2:31`,
		`7 "=>" 3:5-3:7`,
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
	src := "rule a = b\nrule broken = (\nrule b = \"x\"\ntype T = { A }\ntype U = { Text string }\n"
	g, errs := ParsePartial(src)
	if len(errs) != 2 {
		t.Fatalf("errors: %v", errs)
	}
	var names []string
	for _, s := range g.File.Decls {
		names = append(names, s.DeclName())
	}
	if got := strings.Join(names, " "); got != "a b U" {
		t.Errorf("statements %q", got)
	}
	if _, err := Parse(src); err == nil {
		t.Error("Parse accepted the source")
	}
	if _, errs := ParsePartial("rule a = b"); errs != nil {
		t.Errorf("errors for valid source: %v", errs)
	}
}

func TestIsIdentifier(t *testing.T) {
	for s, want := range map[string]bool{
		"a": true, "_x1": false, "名前": false, "Abc": true, "a_b2": true,
		"": false, "1a": false, "a-b": false, "a b": false, "\xff": false, "$a": false,
	} {
		if got := IsIdentifier(s); got != want {
			t.Errorf("IsIdentifier(%q) = %v", s, got)
		}
	}
	for word := range keywords {
		if !IsIdentifier(word) {
			t.Errorf("contextual keyword %q is not an identifier", word)
		}
		if _, err := Parse("rule " + word + ` = "x"`); err != nil {
			t.Errorf("contextual keyword %q cannot be a rule name: %v", word, err)
		}
	}
}

func TestUnicodeIdentifiersAreRejected(t *testing.T) {
	for _, src := range []string{
		`package 名前; rule main = "x"`,
		`rule 名前 = "x"`,
		`type Café = { Text string }`,
		`type T = { 名前 node[string] }`,
		`rule main = 名前`,
		`rule main = 名:"x"`,
		`rule main = {名前 := 1}`,
		`rule main = "x" @é`,
		`rule main = "x" @error(méssage="x")`,
		`rule main = level(expr, 名)`,
		`rule main = "x" => foo(名前)`,
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("accepted Unicode identifier in %q", src)
		}
	}
	for _, src := range []string{
		`rule main = "名前😀"`,
		`rule main = [é-ê]`,
		`rule main = "x" // 名前`,
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("rejected Unicode source text %q: %v", src, err)
		}
	}
}

// TestLongRunsOfInvalidCharacters checks that the lexer handles a long run of characters that
// start no token without recursing per character (which overflowed the stack), and reports the
// run as one error.
func TestLongRunsOfInvalidCharacters(t *testing.T) {
	junk := strings.Repeat("`'😀", 1<<19) // 2M characters
	src := "rule a = \"x\"\n" + junk + "\nrule b = \"y\""
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
		"rule a = " + strings.Repeat("(\n", 1<<20), // used to overflow the stack
		"rule a = " + strings.Repeat("!", n) + "b",
		"rule a = " + strings.Repeat("x:", n) + "b",
		"rule a = b #recover(skip=" + strings.Repeat("c #recover(skip=", n),
		"rule a = b => " + strings.Repeat("- ", n) + "1",
		"rule a = b => " + strings.Repeat("(", n) + "1",
		"rule a = b {" + strings.Repeat("!", n) + "x",
		"rule a = b => foldl(" + strings.Repeat("(x) => foldl(", n),
		"rule a " + strings.Repeat("[]", n) + "T = b",
		"type T ~ " + strings.Repeat("(*", n) + "U",
	} {
		_, errs := ParsePartial(src + "\nrule ok = \"x\"")
		found := false
		for _, e := range errs {
			found = found || e.Msg == "nesting too deep"
		}
		if !found {
			t.Errorf("%.30q...: errors %v", src, errs[:min(len(errs), 3)])
		}
	}
	// Deep nesting below the limit is fine.
	if _, errs := ParsePartial("rule a = " + strings.Repeat("(", 200) + "b" + strings.Repeat(")", 200)); errs != nil {
		t.Errorf("errors %v", errs)
	}
}

// TestErrorEnd checks the end of the errors whose text is known: an escape sequence (from its
// backslash) and a run of invalid characters.
func TestErrorEnd(t *testing.T) {
	for _, tc := range []struct {
		src            string
		line, col, end int
		msg            string
	}{
		{`rule a = "😀\q"`, 1, 12, 14, `unknown escape sequence \q`},
		{`rule a = [a\9]`, 1, 12, 14, `unknown escape sequence \9`},
		{`rule a = "\u{zz}"`, 1, 11, 17, `invalid unicode escape \uzz`},
		{"rule a = \"x\" `'` b", 1, 14, 17, "unexpected characters \"`'`\""},
	} {
		_, errs := ParsePartial(tc.src)
		if len(errs) != 1 {
			t.Errorf("%s: errors %v", tc.src, errs)
			continue
		}
		e := errs[0]
		if e.Msg != tc.msg || e.Pos != (grammar.Pos{Line: tc.line, Col: tc.col}) || e.End != (grammar.Pos{Line: tc.line, Col: tc.end}) {
			t.Errorf("%s: error %q at %v-%v, want %q at %d:%d-%d", tc.src, e.Msg, e.Pos, e.End, tc.msg, tc.line, tc.col, tc.end)
		}
	}
}

// TestLineEnds checks that a line ends at "\n", "\r\n" or a lone "\r": a comment ends there and
// the next token is on the next line. Lone carriage returns used to make everything after the
// first comment part of it.
func TestLineEnds(t *testing.T) {
	for _, nl := range []string{"\n", "\r\n", "\r"} {
		src := strings.Join([]string{"// first", "rule a = b // second", "", "rule b = \"x\""}, nl)
		toks := Tokenize(src)
		var got []string
		for _, tok := range toks {
			got = append(got, fmt.Sprintf("%q %d:%d", tok.Text, tok.Pos.Line, tok.Pos.Col))
		}
		want := `"// first" 1:1 "rule" 2:1 "a" 2:6 "=" 2:8 "b" 2:10 "// second" 2:12 "rule" 4:1 "b" 4:6 "=" 4:8 "x" 4:10`
		if strings.Join(got, " ") != want {
			t.Errorf("%q: tokens %s", nl, strings.Join(got, " "))
		}
		g, errs := ParsePartial(src)
		if errs != nil || len(g.Rules()) != 2 {
			t.Errorf("%q: errors %v, rules %v", nl, errs, g.Rules())
			continue
		}
		// The layout is the same: the comments are kept and the blank line too.
		if got, want := grammar.Format(g), "// first\nrule a = b // second\n\nrule b = \"x\"\n"; got != want {
			t.Errorf("%q: formatted %q", nl, got)
		}
	}
}

// TestIncompletePackageClause checks that a package clause without a name is reported as an
// error, not a panic, and that the definitions after it are parsed.
func TestIncompletePackageClause(t *testing.T) {
	for _, src := range []string{"package", "package ", "package {", "package\nrule a = \"x\"", "package 1\nrule a = \"x\""} {
		g, errs := ParsePartial(src)
		if len(errs) != 1 || !strings.HasPrefix(errs[0].Msg, "expected identifier") {
			t.Errorf("%q: errors %v", src, errs)
		}
		if strings.Contains(src, "rule a") && (len(g.File.Decls) != 1 || g.Rules()[0].Name != "a") {
			t.Errorf("%q: statements %v", src, g.File.Decls)
		}
		if _, err := Parse(src); err == nil {
			t.Errorf("%q: Parse accepted it", src)
		}
	}
}

func TestIsRuleName(t *testing.T) {
	for s, want := range map[string]bool{
		"a": true, "Abc": true, "a_b2": true, "_x": true, "_x1": true, "__x": true, "_1": true,
		"_": false, "": false, "1a": false, "名前": false, "_名": false, "a-b": false, "_a b": false, "\xff": false, "_\xff": false,
	} {
		if got := IsRuleName(s); got != want {
			t.Errorf("IsRuleName(%q) = %v", s, got)
		}
	}
	// Every name it accepts can be written as a rule.
	for _, s := range []string{"a", "_x", "__x", "_1"} {
		if _, err := Parse("rule " + s + ` = "x"`); err != nil {
			t.Errorf("rule %s: %v", s, err)
		}
	}
}
