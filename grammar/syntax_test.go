package grammar_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/starttest"
)

func TestFormatSyntax(t *testing.T) {
	const src = `// grammar documentation
package sample

type Node = { Text node[string] }
rule main Node = {count:=0} x:((v1:"a" => Node{Text: $v1}) #repeat(2, 3) #atomic @error(message="a") #recover(skip=";")) #stream => Node{Text: $x}
rule expr Node = #operators {
 skip " "*
 operand "x" @error(message="x") => Node{}
 level { infix left "+" => Node{} }
} => ...
`
	g, err := pego.ParseGrammar(src)
	if err != nil {
		t.Fatal(err)
	}
	before, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	beforeText := grammar.Format(g)
	out := grammar.FormatSyntax(g)
	for _, gone := range []string{"type Node", "rule main Node", "=>", "@error", "#recover", "x:"} {
		if strings.Contains(out, gone) {
			t.Errorf("kept %q:\n%s", gone, out)
		}
	}
	for _, kept := range []string{"package sample", "// grammar documentation", "count := 0", "#repeat(2, 3)", "#atomic", "#stream", "operators", "infix left"} {
		if !strings.Contains(out, kept) {
			t.Errorf("lost %q:\n%s", kept, out)
		}
	}
	after, _ := grammar.MarshalJSON(g)
	if !bytes.Equal(before, after) || grammar.Format(g) != beforeText {
		t.Fatal("input AST mutated")
	}
	projected, err := pego.ParseGrammar(out)
	if err != nil {
		t.Fatal(err)
	}
	if again := grammar.FormatSyntax(projected); again != out {
		t.Fatalf("projection not stable:\n%s\n%s", out, again)
	}
}

func TestFormatSyntaxNormalizedPragmas(t *testing.T) {
	g, err := pego.ParseGrammar(`rule main = "a"* #stream`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pego.Compile(starttest.MarkAll(g), "main")
	if err != nil {
		t.Fatal(err)
	}
	normalized := p.Grammar()
	before, _ := grammar.MarshalJSON(normalized)
	if out := grammar.FormatSyntax(normalized); !strings.Contains(out, "#stream") {
		t.Fatalf("normalized #stream lost: %s", out)
	}
	after, _ := grammar.MarshalJSON(normalized)
	if !bytes.Equal(before, after) {
		t.Fatal("normalized input AST mutated")
	}
}

func TestFormatSyntaxValueDependentPredicate(t *testing.T) {
	g, err := pego.ParseGrammar(`type T = { N int }
rule main = x:value {$x.N == 1}
rule value T = "x" => T{N: 1}`)
	if err != nil {
		t.Fatal(err)
	}
	out := grammar.FormatSyntax(g)
	if !strings.Contains(out, "{$x.N == 1}") {
		t.Fatalf("predicate lost: %s", out)
	}
	if strings.Contains(out, "x:value") {
		t.Fatalf("capture retained: %s", out)
	}
	proj, err := pego.ParseGrammar(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pego.Compile(starttest.MarkAll(proj), "main"); err == nil {
		t.Fatal("expected value-dependent projection to require the full grammar")
	}
}

func TestFormatSyntaxGrouping(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`rule main = cap:((("a" => A{})))`, `rule main = "a"`},
		{`rule main = (("a" / "b")) "c"`, `rule main = ("a" / "b") "c"`},
		{`rule main = (("a" "b"))*`, `rule main = ("a" "b")*`},
		{`rule main = (("a" / "b")) #atomic`, `rule main = ("a" / "b") #atomic`},
		{`rule main = !(("a" / "b")) "c"`, `rule main = !("a" / "b") "c"`},
		{"rule main = (\n \"a\"\n \"b\"\n)", "rule main = (\n    \"a\"\n    \"b\"\n)"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			g, err := pego.ParseGrammar(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			out := grammar.FormatSyntax(g)
			if strings.TrimSpace(out) != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", out, tc.want)
			}
			projected, err := pego.ParseGrammar(out)
			if err != nil {
				t.Fatal(err)
			}
			if grammar.FormatSyntax(projected) != out {
				t.Fatal("projection not idempotent")
			}
			p, err := pego.Compile(starttest.MarkAll(projected), "main")
			if err != nil {
				t.Fatal(err)
			}
			// The corresponding full grammar has identical matching behavior
			// whenever it does not require an unimplemented inline action.
			full, err := pego.Compile(starttest.MarkAll(g), "main")
			if err != nil {
				return
			}
			for _, input := range []string{"", "a", "b", "c", "ab", "ac", "bc", "abab"} {
				_, originalErr := full.Parse(input, pego.RecognizeOnly())
				_, projectedErr := p.Parse(input, pego.RecognizeOnly())
				if (originalErr == nil) != (projectedErr == nil) {
					t.Fatalf("matching changed for %q: %v / %v", input, originalErr, projectedErr)
				}
			}
		})
	}
}

func TestFormatSyntaxGroupComments(t *testing.T) {
	for _, source := range []string{
		"rule main = (\n \"a\" // keep\n)",
		"rule main = (\n // keep\n \"a\"\n)",
		"rule main = (\n \"a\"\n /* keep */\n)",
	} {
		g, err := pego.ParseGrammar(source)
		if err != nil {
			t.Fatal(err)
		}
		out := grammar.FormatSyntax(g)
		if strings.Count(out, "keep") != 1 {
			t.Fatalf("group comment lost or duplicated: %s", out)
		}
		projected, err := pego.ParseGrammar(out)
		if err != nil {
			t.Fatal(err)
		}
		if again := grammar.FormatSyntax(projected); again != out {
			t.Fatalf("comment layout unstable:\n%s\n%s", out, again)
		}
	}
}
