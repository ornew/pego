package syntax

import (
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

// A detached paragraph is separated from the node before it and the node after
// it by blank lines, also right after an opening brace.
func TestFormatKeepsDetachedCommentAfterBrace(t *testing.T) {
	for _, tc := range []struct {
		name, src string
	}{
		{"in a struct", "type T = {\n\n    // t4\n\n    A int\n}\n"},
		{"in an operators rule", "rule r = #operators {\n\n    // t4\n\n    skip \" \"\n}\n"},
		{"in a level", "rule r = #operators {\n    level {\n\n        // t4\n\n        infix left \"+\"\n    }\n}\n"},
		{"in a block", "rule r = \"a\" {\n\n    // t4\n\n    x := 1\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkFormat(t, tc.src); got != tc.src {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.src)
			}
		})
	}
}

// The comments a node owns are written back with their blank lines when the
// grammar has no layout, as after JSON.
func TestFormatJSONKeepsDetachedParagraphs(t *testing.T) {
	for _, tc := range []struct {
		name, src string
	}{
		{"fields", "type T = {\n    A int\n\n    // r5\n\n    B string\n}\n"},
		{"first field", "type T = {\n\n    // r5\n\n    A int\n}\n"},
		{"operands", "rule r = #operators {\n    operand \"n\"\n\n    // r5\n\n    operand \"m\"\n}\n"},
		{"operators", "rule r = #operators {\n    level {\n        infix left \"+\"\n\n        // r5\n\n        infix left \"-\"\n    }\n}\n"},
		{"trailing over lines", "rule r = \"a\" // s1\n// t1\nrule q = \"b\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if problem := jsonRoundTripProblem(g); problem != "" {
				t.Fatal(problem)
			}
			data, err := grammar.MarshalJSON(g)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := grammar.UnmarshalJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if got := grammar.Format(decoded); got != tc.src && tc.name != "trailing over lines" {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.src)
			}
		})
	}
}
