package syntax

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

func TestUnicodeScalarEscapes(t *testing.T) {
	for _, escape := range []string{`\uD800`, `\uDBFF`, `\uDC00`, `\uDFFF`, `\u{D800}`, `\u{DFFF}`, `\u{110000}`, `\u{7FFFFFFF}`, `\u{80000000}`, `\u{FFFFFFFF}`, `\u{100000000}`, `\u{FFFFFFFFFFFFFFFF}`} {
		for _, expr := range []string{`"` + escape + `"`, `(?` + escape + `)`, `(?^` + escape + `)`} {
			t.Run(expr, func(t *testing.T) {
				if e, err := ParseExpr(expr); err == nil || e != nil {
					t.Fatalf("accepted invalid escape: %v, %v", e, err)
				}
			})
		}
		for _, source := range []string{`def main="x" -> "` + escape + `"`, `def main=_|_ #error(message="` + escape + `")`} {
			if g, err := Parse(source); err == nil || g != nil {
				t.Fatalf("accepted invalid escape in term: %v, %v", g, err)
			}
		}
	}
	// Adjacent UTF-16 surrogate escapes are not a spelling of a scalar.
	if _, err := ParseExpr(`"\uD83D\uDE00"`); err == nil {
		t.Fatal("accepted a surrogate pair")
	}
	for _, r := range []rune{0, 0x7f, 0xd7ff, 0xe000, 0xfdd0, 0xfffd, 0xfffe, 0xffff, 0x10000, utf8.MaxRune} {
		spellings := []string{fmt.Sprintf(`\u{%X}`, r), fmt.Sprintf(`\u{000000%X}`, r)}
		if r <= 0xffff {
			spellings = append(spellings, fmt.Sprintf(`\u%04X`, r))
		}
		for _, escape := range spellings {
			for _, source := range []string{`"` + escape + `"`, `(?` + escape + `)`} {
				e, err := ParseExpr(source)
				if err != nil {
					t.Fatalf("valid scalar %U: %v", r, err)
				}
				switch e := e.(type) {
				case *grammar.Literal:
					if e.Value != string(r) {
						t.Fatalf("%s: literal %q, want %q", source, e.Value, string(r))
					}
				case *grammar.CharClass:
					if len(e.Ranges) != 1 || e.Ranges[0] != (grammar.CharRange{Lo: r, Hi: r}) {
						t.Fatalf("%s: ranges %v", source, e.Ranges)
					}
				}
				if _, err := ParseExpr(grammar.FormatExpr(e)); err != nil {
					t.Fatalf("valid scalar failed format round trip: %v", err)
				}
			}
		}
	}
}

func TestUnicodeScalarEscapeDiagnostic(t *testing.T) {
	for _, escape := range []string{`\uD800`, `\u{110000}`, `\u{FFFFFFFF}`} {
		_, errs := ParsePartial(`def main="😀` + escape + `"` + "\ndef after=\"ok\"")
		if len(errs) != 1 || errs[0].Pos != (grammar.Pos{Line: 1, Col: 12}) || errs[0].End != (grammar.Pos{Line: 1, Col: 12 + len(escape)}) || !strings.Contains(errs[0].Msg, "invalid unicode escape") {
			t.Fatalf("%s: diagnostics %v", escape, errs)
		}
	}
	g, err := Parse("def main =\n    (?a-z)")
	if err != nil {
		t.Fatal(err)
	}
	cc := g.Rules()[0].Expr.(*grammar.CharClass)
	cc.Ranges[0].Hi = 0xd800
	if err, ok := grammar.Validate(g).(*grammar.ValidationError); !ok || err.Pos != (grammar.Pos{Line: 2, Col: 5}) {
		t.Fatalf("class range position was lost: %v", err)
	}
}

func BenchmarkUnicodeEscapeSyntax(b *testing.B) {
	for _, tc := range []struct{ name, source string }{
		{"Plain", `def main="` + strings.Repeat("a", 100) + `"`},
		{"String", `def main="` + strings.Repeat(`\u{1F600}`, 100) + `"`},
		{"Classes", `def main=` + strings.Repeat(`(?\uD7FF-\uE000) `, 100)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Parse(tc.source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
