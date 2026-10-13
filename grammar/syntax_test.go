package grammar_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ornew/pego"
	"github.com/ornew/pego/grammar"
)

func TestFormatSyntax(t *testing.T) {
	const src = `// grammar documentation
package sample

type Node struct { Text Match }
rule main Node = {count:=0} x:(("a" => Node{Text: $1}) #repeat(2, 3) #atomic @error(message="a") #recover(skip=";")) #stream => Node{Text: $x}
rule expr Node = operators {
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
	for _, gone := range []string{"type Node", "rule main Node", "=>", "@error", "#recover"} {
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
	p, err := pego.Compile(g, "main")
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
	g, err := pego.ParseGrammar(`type T struct { N int }
rule main = x:value {$x.N == 1}
rule value T = "x" => T{N: 1}`)
	if err != nil {
		t.Fatal(err)
	}
	out := grammar.FormatSyntax(g)
	if !strings.Contains(out, "{$x.N == 1}") {
		t.Fatalf("predicate lost: %s", out)
	}
	if _, err := pego.CompileSource(out, "main"); err == nil {
		t.Fatal("expected value-dependent projection to require the full grammar")
	}
}
