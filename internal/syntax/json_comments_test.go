package syntax

import (
	"testing"

	"github.com/ornew/zeroparse/grammar"
)

// A grammar decoded from JSON has no layout, but JSON carries the comments the
// nodes own, and Format writes them again for the nodes on lines of their own.
func TestFormatJSONWritesOwnedComments(t *testing.T) {
	const src = `// Package doc.
package j

// Leading of T.
type T = {
    A int // trailing A
    // leading B
    B string
}

// Detached.

// Leading of main.
rule main = a:"a" => T{A: 1, B: "x"} // trailing main

/* Leading of other. */
rule other = "x" // trailing other

rule ops = #operators {
    // leading skip
    skip " " // trailing skip
    // leading operand
    operand "n" // trailing operand
    // leading level
    level {
        // leading op
        infix left "+" // trailing op
    }
}
`
	g, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	data, err := grammar.MarshalJSON(g)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := grammar.UnmarshalJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	out := grammar.Format(decoded)
	again, err := Parse(out)
	if err != nil {
		t.Fatalf("the result does not parse: %v\n%s", err, out)
	}
	if a, b := commentTexts(g), commentTexts(again); len(a) != len(b) {
		t.Errorf("comments\n got %q\nwant %q\n%s", b, a, out)
	}
	if diffs := ownerDiffs(g, again); len(diffs) > 0 {
		t.Errorf("owners changed\n%v\n%s", diffs, out)
	}
	if second := grammar.Format(again); second != out {
		t.Errorf("formatting is not idempotent\nfirst:\n%s\nsecond:\n%s", out, second)
	}
	if out != src {
		t.Errorf("got:\n%s\nwant:\n%s", out, src)
	}
}
