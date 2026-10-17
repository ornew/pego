package lsp

import (
	"strings"
	"testing"

	"github.com/ornew/pego/internal/syntax"
)

func TestNewSourceServices(t *testing.T) {
	src := "type N terminal\n/* A number\nwith a name. */\nrule main = v1:digit @error(message=\"number\") => $v1\nrule expr N = operators {\n    operand digit\n    level { infix left \"+\" => $lhs }\n}\nrule digit N = [0-9]+ #atomic\n"
	a := analyze(src)
	if len(a.diags) != 0 {
		t.Fatalf("diagnostics: %+v", a.diags)
	}
	main, expr := a.rules["main"], a.rules["expr"]
	if main == nil || expr == nil {
		t.Fatalf("rules: %+v", a.rules)
	}
	if !strings.HasPrefix(src[main.start:main.end], "rule main =") || !strings.HasPrefix(src[expr.start:expr.end], "rule expr N = operators") || strings.Contains(src[expr.start:expr.end], "#atomic") {
		t.Fatalf("definition ranges: main=%q expr=%q", src[main.start:main.end], src[expr.start:expr.end])
	}
	if main.doc != "A number\nwith a name." {
		t.Fatalf("documentation: %q", main.doc)
	}
	if hover := a.ruleHover(expr); !strings.Contains(hover, "rule expr N = operators") {
		t.Fatalf("hover: %s", hover)
	}
	for _, tok := range a.toks {
		if tok.Kind == syntax.TokenComment && strings.HasPrefix(tok.Text, "/*") {
			if items := a.complete(tok.start + len("/* A number\n")); len(items) != 0 {
				t.Fatalf("completion inside block comment: %+v", items)
			}
		}
	}
}

func TestNewSourceCommentSemanticLines(t *testing.T) {
	a := analyze("/*😀\r\nsecond\r\n*/\nrule main = \"x\"")
	data := a.semanticTokens()
	line, col := 0, 0
	var lengths []int
	for i := 0; i < len(data); i += 5 {
		if data[i] != 0 {
			col = 0
		}
		line += data[i]
		col += data[i+1]
		if semTokenType(data[i+3]) == semComment {
			if line != len(lengths) || col != 0 {
				t.Fatalf("comment position: %d:%d", line, col)
			}
			lengths = append(lengths, data[i+2])
		}
	}
	if len(lengths) != 3 || lengths[0] != 4 || lengths[1] != 6 || lengths[2] != 2 {
		t.Fatalf("UTF-16 comment lengths: %v", lengths)
	}
}

func TestNewSourceAttributeAndCommentContexts(t *testing.T) {
	for _, prefix := range []string{"", "rule prior = \"x\"\n"} {
		src := prefix + "rule main = \"x\" #recover(skip=(\nrule digit = [0-9]"
		a := analyze(src)
		off := strings.Index(src, "skip=(") + len("skip=(")
		found := false
		for _, item := range a.complete(off) {
			if item.Label == "digit" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing recovery rule completion: %+v", a.complete(off))
		}
	}
	src := `type N = { X Match }
rule digit = [0-9]
rule expr N = operators { operand digit:digit => N{X:$digit} }`

	a := analyze(src)
	if len(a.diags) != 0 {
		t.Fatalf("diagnostics: %+v", a.diags)
	}
	d := a.rules["expr"]
	if d == nil || !strings.HasPrefix(src[d.start:d.end], "rule expr N = operators") {
		t.Fatalf("definition: %+v", d)
	}
	off := strings.Index(src, "N{X:")
	text, _, _, ok := a.hoverAt(off)
	if !ok || !strings.Contains(text, "type N =") {
		t.Fatalf("constructor hover: %s", text)
	}
	prefix := src[:strings.Index(src, "operand digit")] + "operand "
	b := analyze(prefix)
	found := false
	for _, item := range b.complete(len(prefix)) {
		if item.Label == "operand" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing Operators Rule context: %+v", b.complete(len(prefix)))
	}
}

func TestNewSourceFirstCaptureCompletion(t *testing.T) {
	src := "type N = { F Match }\nrule main = x:\"a\" => N{F: $x}"
	a := analyze(src)
	off := strings.Index(src, "$x") + 1
	var labels []string
	for _, item := range a.complete(off) {
		labels = append(labels, item.Label)
	}
	if len(labels) != 1 || labels[0] != "x" {
		t.Fatalf("capture completion: %v", labels)
	}
}

func TestRecoverPragmaCompletion(t *testing.T) {
	src := `rule main = "a" #`
	a := analyze(src)
	found := false
	for _, item := range a.complete(len(src)) {
		if item.Label == "recovery" {
			t.Fatal("unexpected pragma completion")
		}
		if item.Label == "recover" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing recover completion: %+v", a.complete(len(src)))
	}
}

func TestReservedNamesExcludedFromCompletion(t *testing.T) {
	for _, src := range []string{
		`rule _private = "x"; rule main = `,
		`type _Hidden = {}; rule main = "x" => `,
		`rule main = _private:"x" => $`,
		`rule main = xs:"x"* => map($xs, (_private) => $`,
	} {
		a := analyze(src)
		for _, item := range a.complete(len(src)) {
			if strings.HasPrefix(item.Label, "_") {
				t.Fatalf("reserved completion %q in %s", item.Label, src)
			}
		}
	}
}
