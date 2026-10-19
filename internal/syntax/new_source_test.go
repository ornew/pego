package syntax

import (
	"strings"
	"testing"

	"github.com/ornew/pego/grammar"
)

func TestExpressionRuleSource(t *testing.T) {
	src := `package example
type Num = { Text string }
type Box = { Value Num }
rule main Box = (
    (n:number {x := len($n)} {x > 0}) @error(message="expected a box")
) => Box{Value: $n}
rule number Num = t:([0-9]+) #atomic => Num{Text: text($t)}
rule expr Num = operators {
    operand number
    level additive { infix left "+" => $lhs }
}
rule inferred = number:number => $number
`
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	rs := g.Rules()
	if len(rs) != 4 || rs[0].Type.(*grammar.TypeRef).Name != "Box" || rs[3].Type != nil {
		t.Fatalf("headers: %+v", rs)
	}
	if _, ok := rs[0].Action.(*grammar.New); !ok {
		t.Fatalf("constructor: %T", rs[0].Action)
	}
	capture, ok := rs[1].Expr.(*grammar.Capture)
	if !ok {
		t.Fatalf("capture: %T", rs[1].Expr)
	}
	if _, ok := capture.Expr.(*grammar.Atomic); !ok {
		t.Fatalf("atomic: %T", capture.Expr)
	}
	if _, ok := rs[2].Expr.(*grammar.OperatorsRule); !ok {
		t.Fatalf("operators rule: %T", rs[2].Expr)
	}
	first, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	formatted := grammar.Format(g)
	again, err := Parse(formatted)
	if err != nil {
		t.Fatalf("formatted source: %v\n%s", err, formatted)
	}
	second, err := grammar.MarshalJSON(again)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("AST changed after formatting\n%s\n%s\n%s", first, second, formatted)
	}
	if stable := grammar.Format(again); formatted != stable {
		t.Fatalf("unstable format\n%s\n%s", formatted, stable)
	}
}

func TestExpressionRuleRejectsRemovedForms(t *testing.T) {
	for _, src := range []string{
		`def main = "x"`,
		`rule main() { "x" }`,
		`rule main: Match { "x" }`,
		`rule main { "x" -> $1 }`,
		`rule main { [true] }`,
		`rule main { "x" [Match{}] }`,
		`rule main { "x" @atomic(value="ignored") }`,
		`rule main { "x" => new Match{} }`,
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("accepted %s", src)
		}
	}
}

func TestExpressionRuleAttributeOrdering(t *testing.T) {
	g, err := Parse(`rule main = [a-z]+ #atomic @error(message="name") #recover(skip=";")`)
	if err != nil {
		t.Fatal(err)
	}
	recovery, ok := g.Rules()[0].Expr.(*grammar.Recovery)
	if !ok {
		t.Fatalf("recovery: %T", g.Rules()[0].Expr)
	}
	attr := recovery.Expr.(*grammar.Attributed)
	if len(attr.Attrs) != 1 || attr.Attrs[0].Name != "error" {
		t.Fatalf("attributes: %+v", attr.Attrs)
	}
	if _, ok := attr.Expr.(*grammar.Atomic); !ok {
		t.Fatalf("atomic: %T", attr.Expr)
	}
}

func TestMissingPragmaSourceAndFormat(t *testing.T) {
	const source = `rule main = ";" @error(message="semicolon") #missing $$`
	g, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	seq := g.Rules()[0].Expr.(*grammar.Seq)
	attrs := seq.Items[0].(*grammar.Attributed).Attrs
	if len(attrs) != 2 || attrs[0].Name != "error" || attrs[1].Name != "missing" || !attrs[1].Pragma {
		t.Fatalf("attributes: %+v", attrs)
	}
	formatted := grammar.Format(g)
	if formatted != source+"\n" {
		t.Fatalf("format changed pragma: %q", formatted)
	}
	if _, err := Parse(formatted); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{`rule main = ";" #missing()`, `rule main = ";" #missing(1)`, `rule main = ";" #missing(foo="b")`} {
		if _, err := Parse(source); err == nil || !strings.Contains(err.Error(), "takes no arguments") {
			t.Fatalf("accepted #missing arguments in %q: %v", source, err)
		}
	}
}

func TestExpressionRuleAnnotationsPreserveBodyComments(t *testing.T) {
	for _, suffix := range []string{"@error(message=\"oops\")", "#atomic"} {
		src := "rule main = (\n \"a\"\n // keep me\n \"b\"\n) " + suffix + "\n"
		g, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		formatted := grammar.Format(g)
		if !strings.Contains(formatted, "// keep me") {
			t.Fatalf("lost comment:\n%s", formatted)
		}
		again, err := Parse(formatted)
		if err != nil {
			t.Fatal(err)
		}
		if stable := grammar.Format(again); stable != formatted {
			t.Fatalf("unstable format:\n%s\n%s", formatted, stable)
		}
	}
}

func TestExpressionRulePartialRecovery(t *testing.T) {
	g, errs := ParsePartial("rule bad = )\nrule good = \"x\" #atomic")
	if len(errs) != 1 || len(g.Rules()) != 1 || g.Rules()[0].Name != "good" {
		t.Fatalf("partial parse: %v, %+v", errs, g.Rules())
	}
	if _, ok := g.Rules()[0].Expr.(*grammar.Atomic); !ok {
		t.Fatalf("lost pragma during recovery: %T", g.Rules()[0].Expr)
	}
}

func TestRecoverPragmaSpelling(t *testing.T) {
	g, err := Parse(`rule main = "a" #recover(skip="b")`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Rules()[0].Expr.(*grammar.Recovery); !ok {
		t.Fatalf("recovery expression: %T", g.Rules()[0].Expr)
	}
	formatted := grammar.Format(g)
	if !strings.Contains(formatted, "#recover(skip=") {
		t.Fatalf("format: %s", formatted)
	}
	if _, err := Parse(formatted); err != nil {
		t.Fatalf("formatted source: %v", err)
	}
	if _, err := Parse(`rule main = "a" #recovery(skip="b")`); err == nil {
		t.Fatal("accepted unsupported pragma spelling")
	}
}
