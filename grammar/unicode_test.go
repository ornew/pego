package grammar

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateUnicodeRanges(t *testing.T) {
	for _, r := range []rune{-1, -2147483648, 0xd800, 0xdbff, 0xdc00, 0xdfff, utf8.MaxRune + 1, 2147483647} {
		for _, field := range []string{"lo", "hi"} {
			cr := CharRange{Lo: 0, Hi: utf8.MaxRune}
			if field == "lo" {
				cr.Lo = r
			} else {
				cr.Hi = r
			}
			g := expressionGrammar(&CharClass{Ranges: []CharRange{{Lo: 'a', Hi: 'z'}, cr}})
			before, err := MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			path := "$.statements[0].expr.ranges[1]." + field
			assertValidationError(t, Validate(g), path)
			if out, err := UnmarshalJSON(before); out != nil || err == nil {
				t.Fatalf("JSON accepted invalid endpoint: %v, %v", out, err)
			} else {
				assertValidationError(t, err, path)
			}
			after, err := MarshalJSON(g)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("validation mutated range")
			}
		}
	}
	g := expressionGrammar(&CharClass{Ranges: []CharRange{{Lo: 'z', Hi: 'a'}}})
	assertValidationError(t, Validate(g), "$.statements[0].expr.ranges[0].hi")
	// Scalar endpoints can span the surrogate interval. Empty AST classes,
	// overlapping ranges and negation retain their existing meanings.
	for _, ranges := range [][]CharRange{nil, {{Lo: 0, Hi: utf8.MaxRune}}, {{Lo: 0xd7ff, Hi: 0xe000}}, {{Lo: 0xfffd, Hi: 0xffff}, {Lo: 0xfffe, Hi: utf8.MaxRune}}} {
		for _, negated := range []bool{false, true} {
			g := expressionGrammar(&CharClass{Ranges: ranges, Negated: negated})
			if err := Validate(g); err != nil {
				t.Fatal(err)
			}
			data, err := MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := UnmarshalJSON(data); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestUnicodeRangeErrorPosition(t *testing.T) {
	e := &CharClass{Pos: Pos{Line: 3, Col: 7}, Ranges: []CharRange{{Lo: 0xd800, Hi: 0xdfff}}}
	err := Validate(expressionGrammar(e))
	assertValidationError(t, err, "$.statements[0].expr.ranges[0].lo")
	if e := err.(*ValidationError); e.Pos != (Pos{Line: 3, Col: 7}) || !strings.HasPrefix(e.Error(), "3:7:") {
		t.Fatal(e)
	}
}
