package syntax_test

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
	"github.com/ornew/pego/internal/engine"
	"github.com/ornew/pego/internal/starttest"
	"github.com/ornew/pego/internal/syntax"
)

func TestExpressionSourceRoundTrip(t *testing.T) {
	for _, src := range []string{
		`rule main = [0-9]+ {x := 1} {x < 10} => ...`,
		`rule main = [^a-zA-Z\)\#]`,
		`package demo; rule main = "a"; type A = { X A; Y A }; rule next = "b"`,
		"rule main = (\n    \"a\"\n    / \"b\"\n)\nrule next = \"c\"",
		`rule main = operators { skip " "*; operand num; level { infix left "+" => ...; postfix "!" }; level power { infix right "^" } }`,
		"rule main = operators {\n    operand \"a\"\n} => ...",
		"rule main = operators {\n    operand \"a\"\n} => ...",
		`rule main = (a => A{}) / (b => B{}) => C{}`,
		`rule main = (a => A{}) => C{}`,
		`rule main = (a => ...)`,
		`rule main = a:a => foldl($a, 0, (acc, item) => acc + item)`,
	} {
		t.Run(src, func(t *testing.T) {
			g, err := syntax.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			formatted := grammar.Format(g)
			g2, err := syntax.Parse(formatted)
			if err != nil {
				t.Fatalf("formatted source invalid: %v\n%s", err, formatted)
			}
			if again := grammar.Format(g2); again != formatted {
				t.Fatalf("format is not stable:\n%s\n%s", formatted, again)
			}
			data, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := grammar.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := syntax.Parse(grammar.Format(decoded)); err != nil {
				t.Fatalf("JSON format invalid: %v", err)
			}
		})
	}
}

func TestExpressionSourceBoundaries(t *testing.T) {
	for _, src := range []string{
		`rule a = "a" rule b = "b"`,
		"rule a = \"a\"\n/ \"b\"",
		"rule a = \"a\"\n\u00a0\"b\"",
		"rule a = \"a\"\n/*comment*/ \"b\"",
		"rule a = \"(\"\n\"x\"",
		"rule a = \"{\"\n\"x\"",
		"rule a = (\"(\" )\n\"x\"",
		`type A = { X A Y A }`,
		"rule a = operators { operand \"a\" }\n=> ...",
		"rule a = operators { operand \"a\" } =>\n...",
		`rule a = a => A{} => B{}`,
		`rule a = a => (B => C)`,
		`#atomic rule a = "a"`,
	} {
		t.Run(src, func(t *testing.T) {
			if _, err := syntax.Parse(src); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
}

func TestExpressionSourceInlineActionsCompile(t *testing.T) {
	for _, src := range []string{
		`rule main = ("a" => ...)`,
		`rule main = x:("a" => "x")`,
		`rule main = ("a" => "x")*`,
		`rule main = "a" #recover(skip=("b" => ...))`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Compile(starttest.MarkAll(g), engine.Options{}); err != nil {
			t.Fatalf("inline action in %s: %v", src, err)
		}
	}
}

func TestExpressionSourceHeaderComments(t *testing.T) {
	for _, src := range []string{
		`rule /*header*/ main = "a"`,
		`type /*header*/ A = { Text string }; rule main = "a"`,
		`package /*header*/ demo; rule main = "a"`,
		`rule main = operators { operand "a"; level /*header*/ { infix left "+" } }`,
		`rule main = operators { operand "a"; level { infix /*header*/ left "+" } }`,
	} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatalf("parsing should preserve header comments: %v", err)
		}
		if len(g.File.HeaderComments) != 1 {
			t.Fatalf("header comments: %d", len(g.File.HeaderComments))
		}
		formatted := grammar.Format(g)
		again, parseErr := syntax.Parse(formatted)
		if parseErr != nil {
			t.Fatalf("header format no longer parses: %v\n%s", parseErr, formatted)
		}
		if len(again.File.HeaderComments) == 0 {
			t.Fatalf("formatter removed header placement error:\n%s", formatted)
		}
		if _, err := engine.Compile(starttest.MarkAll(again), engine.Options{}); err == nil {
			t.Fatal("formatted header became compilable")
		}
		_, err = engine.Compile(starttest.MarkAll(g), engine.Options{})
		if err == nil || !strings.Contains(err.Error(), "comments are not allowed inside statement headers") {
			t.Fatalf("expected positioned semantic rejection, got %v", err)
		}
	}
	valid := `type N = { Value int }
var x int
/*before body*/
/*inside*/
/*predicate*/
/*after body*/
/*before action*/
/*argument*/
rule main = v1:"a" "b" {x := 1} => N{Value: len($v1)} /*after action*/`
	g, err := syntax.Parse(valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.File.HeaderComments) != 0 {
		t.Fatal("expression comments marked as headers")
	}
	if _, err := engine.Compile(starttest.MarkAll(g), engine.Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestExpressionSourceDefaultCST(t *testing.T) {
	var trees []string
	for _, src := range []string{`rule main = "a" "b"`, `rule main = "a" "b" => ...`} {
		g, err := syntax.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		p, err := engine.Compile(starttest.MarkAll(g), engine.Options{})
		if err != nil {
			t.Fatal(err)
		}
		n, err := p.Parse("main", "ab")
		if err != nil {
			t.Fatal(err)
		}
		trees = append(trees, n.String())
	}
	if trees[0] != trees[1] || trees[0] != `("a" "b")` {
		t.Fatalf("default CST differs: %v", trees)
	}
}
