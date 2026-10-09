package syntax

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ornew/pego/grammar"
)

func invalidDecimalSpellings() []string {
	maxInt := uint64(^uint(0) >> 1)
	return []string{"١", "１２", "𝟙", "1١", "١2", strconv.FormatUint(maxInt+1, 10), "18446744073709551616", strings.Repeat("9", 4096)}
}

func TestDecimalTokenErrors(t *testing.T) {
	for _, digits := range invalidDecimalSpellings() {
		for _, prefix := range []string{`def main="a" -> $`, `def main="a" -> `, `def main="a"{`, `def main="a" [`, `def main="a" #custom(n=`} {
			suffix := ""
			switch prefix {
			case `def main="a"{`:
				suffix = "}"
			case `def main="a" [`:
				suffix = " == 0]"
			case `def main="a" #custom(n=`:
				suffix = ")"
			}
			source := prefix + digits + suffix
			if g, err := Parse(source); g != nil || err == nil {
				t.Fatalf("accepted invalid decimal %q: %v, %v", source, g, err)
			}
			g, errs := ParsePartial(source + "\ndef after=\"ok\"")
			col := utf8.RuneCountInString(prefix) + 1
			if strings.HasSuffix(prefix, "$") {
				col--
			}
			if len(errs) != 1 || errs[0].Pos != (grammar.Pos{Line: 1, Col: col}) || errs[0].End != (grammar.Pos{Line: 1, Col: utf8.RuneCountInString(prefix+digits) + 1}) {
				t.Fatalf("%q: wrong numeric diagnostic: %v", source, errs)
			}
			if rules := g.Rules(); len(rules) == 0 || rules[len(rules)-1].Name != "after" {
				t.Fatalf("lost subsequent rule after invalid decimal: %q", source)
			}
		}
	}
}

func TestDecimalTokenBoundaries(t *testing.T) {
	for _, spelling := range []string{"0", "00", "001", "123", strconv.Itoa(int(^uint(0) >> 1))} {
		want, _ := strconv.Atoi(spelling)
		for _, source := range []string{spelling, "$" + spelling} {
			errs := &errorList{}
			tokens := lex(source, errs)
			if len(errs.list) != 0 || len(tokens) != 2 || tokens[0].num != want {
				t.Fatalf("%q: %v, %v", source, tokens, errs.list)
			}
			if tok := Tokenize(source)[0]; tok.Text != spelling {
				t.Fatalf("tokenizer rewrote %q as %q", source, tok.Text)
			}
		}
	}
	for _, digits := range invalidDecimalSpellings() {
		if tok := Tokenize("$" + digits)[0]; tok.Kind != TokenIndex || tok.Text != digits {
			t.Fatalf("tokenizer rewrote invalid index %q as %+v", digits, tok)
		}
	}
	if _, err := Parse(`def 名前١=x١:"a" -> $x١`); err != nil {
		t.Fatalf("Unicode digits in identifiers were rejected: %v", err)
	}
}

func BenchmarkDecimalSource(b *testing.B) {
	for _, tc := range []struct{ name, source string }{
		{"Plain", `def main="a"`},
		{"Integers", "def main=\"a\" -> " + strings.Repeat("123 + ", 100) + "0"},
		{"Indices", "def main=\"a\" -> " + strings.Repeat("$1 + ", 100) + "$0"},
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
